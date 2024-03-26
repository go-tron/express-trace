package kuaidi100

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"github.com/go-playground/validator/v10"
	"github.com/go-resty/resty/v2"
	baseError "github.com/go-tron/base-error"
	"github.com/go-tron/config"
	expressTrace "github.com/go-tron/express-trace"
	localTime "github.com/go-tron/local-time"
	"github.com/go-tron/logger"
	"strconv"
	"strings"
)

var (
	ErrorParam          = baseError.SystemFactory("3011", "快递查询服务参数错误:{}")
	ErrorRequest        = baseError.SystemFactory("3012", "快递查询服务连接失败:{}")
	ErrorResponse       = baseError.SystemFactory("3013", "快递查询服务返回失败:{}")
	ErrorFail           = baseError.SystemFactory("3014")
	ErrorCallbackParams = baseError.Factory("3015", "缺少参数")
	ErrorSign           = baseError.New("3016", "签名验证失败")
)

const (
	StateInTransit  = "0"
	StateAccepted   = "1"
	StateException  = "2"
	StateDelivered  = "3"
	StateCanceled   = "4"
	StateInProgress = "5"
	StateReturned   = "6"
	StateTransfer   = "7"
	StateClearance  = "8"
	StateRefused    = "14"
)

var stateCode = map[string]string{
	StateInTransit:  "inTransit",
	StateAccepted:   "accepted",
	StateException:  "exception",
	StateDelivered:  "delivered",
	StateCanceled:   "canceled",
	StateInProgress: "inProgress",
	StateReturned:   "returned",
	StateTransfer:   "transfer",
	StateClearance:  "clearance",
	StateRefused:    "refused",
}

func StateCode(code string) string {
	return stateCode[code]
}

var validate *validator.Validate

func init() {
	validate = validator.New()
}

func NewWithConfig(c *config.Config) *Kuaidi100 {
	return New(&Kuaidi100{
		key:          c.GetString("kuaidi100.key"),
		Customer:     c.GetString("kuaidi100.customer"),
		SubscribeUrl: c.GetString("kuaidi100.subscribeUrl"),
		SignSalt:     c.GetString("kuaidi100.signSalt"),
		Logger:       logger.NewZapWithConfig(c, "kuaidi100", "error"),
	})
}

func New(c *Kuaidi100) *Kuaidi100 {
	if c == nil {
		panic("config 必须设置")
	}
	if c.key == "" {
		panic("key 必须设置")
	}
	if c.Customer == "" {
		panic("Customer 必须设置")
	}
	if c.SignSalt == "" {
		panic("SignSalt 必须设置")
	}
	if c.SubscribeUrl == "" {
		panic("SubscribeUrl 必须设置")
	}
	if c.Logger == nil {
		panic("Logger 必须设置")
	}
	return c
}

type Kuaidi100 struct {
	key          string
	Customer     string
	SubscribeUrl string
	SignSalt     string
	Logger       logger.Logger
}

type Response struct {
	Result     bool   `json:"result"`
	ReturnCode string `json:"returnCode"`
	Message    string `json:"message"`
}

func (c *Kuaidi100) Subscribe(req *expressTrace.SubscribeReq) (err error) {

	var resBody = ""
	defer func() {
		c.Logger.Info("",
			c.Logger.Field("number", req.Number),
			c.Logger.Field("error", err),
			c.Logger.Field("response", resBody),
		)
	}()

	if err := validate.Struct(req); err != nil {
		return ErrorParam(err)
	}

	var data = map[string]interface{}{
		"company": "", //req.Company,
		"number":  req.Number,
		"key":     c.key,
		"parameters": map[string]interface{}{
			"autoCom":     "1",
			"callbackurl": c.SubscribeUrl + "?orderId=" + strconv.FormatInt(req.OrderId, 10),
			"salt":        c.SignSalt,
			"resultv2":    "0",
		},
	}

	param, _ := json.Marshal(data)

	c.Logger.Info("开始请求",
		c.Logger.Field("number", req.Number),
	)
	url := "https://poll.kuaidi100.com/poll"

	request := resty.New().R()
	request = request.SetHeaders(map[string]string{
		"Content-Type": "application/x-www-form-urlencoded",
	})
	request = request.SetQueryParam("schema", "json")
	request = request.SetQueryParam("param", string(param))
	response, err := request.Post(url)
	if err != nil {
		return ErrorRequest(err)
	}
	resBody = response.String()
	var res Response
	if err := json.Unmarshal(response.Body(), &res); err != nil {
		return ErrorResponse(err)
	}

	if !res.Result {
		var errorMsg = "请求失败"
		if res.Message != "" {
			errorMsg = res.Message
		}
		return ErrorFail(errorMsg)
	}

	return nil
}

type SubscribeCallback struct {
	Status     string `json:"status"`
	Message    string `json:"message"`
	LastResult struct {
		Message string `json:"message"`
		Nu      string `json:"nu"`
		Ischeck string `json:"ischeck"`
		Com     string `json:"com"`
		Data    []struct {
			Time    *localTime.Time `json:"time"`
			Context string          `json:"context"`
		} `json:"data"`
		State     string `json:"state"`
		RouteInfo struct {
			From struct {
				Number string `json:"number"`
				Name   string `json:"name"`
			} `json:"from"`
			Cur struct {
				Number string `json:"number"`
				Name   string `json:"name"`
			} `json:"cur"`
			To interface{} `json:"to"`
		} `json:"routeInfo"`
		IsLoop bool `json:"isLoop"`
	} `json:"lastResult"`
}

func (c *Kuaidi100) SubscribeCallback(orderId int64, data map[string]string) (res *expressTrace.SubscribeRes, err error) {
	if orderId == 0 {
		return nil, ErrorCallbackParams("orderId")
	}
	if data["param"] == "" {
		return nil, ErrorCallbackParams("param")
	}
	if data["sign"] == "" {
		return nil, ErrorCallbackParams("sign")
	}

	signStr := data["param"] + c.SignSalt
	hash := md5.New()
	hash.Write([]byte(signStr))
	sign := strings.ToUpper(hex.EncodeToString(hash.Sum(nil)))
	if data["sign"] != sign {
		return nil, ErrorSign
	}

	callback := &SubscribeCallback{}
	if err := json.Unmarshal([]byte(data["param"]), callback); err != nil {
		return nil, err
	}

	signed, err := strconv.Atoi(callback.LastResult.Ischeck)
	if err != nil {
		return nil, err
	}

	var lastTraceInfo = ""
	var lastTraceTime *localTime.Time
	if len(callback.LastResult.Data) > 0 {
		lastTraceInfo = callback.LastResult.Data[0].Context
		lastTraceTime = callback.LastResult.Data[0].Time
	}

	var traces = make([]expressTrace.Trace, 0)
	for _, v := range callback.LastResult.Data {
		traces = append(traces, expressTrace.Trace{
			Time: v.Time,
			Info: v.Context,
		})
	}

	return &expressTrace.SubscribeRes{
		OrderId:       orderId,
		Number:        callback.LastResult.Nu,
		Signed:        signed,
		Status:        StateCode(callback.LastResult.State),
		LastTraceInfo: lastTraceInfo,
		LastTraceTime: lastTraceTime,
		Traces:        traces,
		CompanyName:   CompanyCodes(callback.LastResult.Com),
		CompanyCode:   callback.LastResult.Com,
	}, nil
}

var companyCodes = map[string]string{"100mexpress": "百米快运", "1ziton": "一智通", "360lion": "360 Lion Express", "a2u": "A2U速递", "aaacooper": "AAA Cooper Transportation", "aae": "AAE-中国件", "abcglobal": "全球快运", "abf": "ABF", "abs": "ABS courier \u0026 freight system", "abxexpress_my": "ABX Express", "acommerce": "aCommerce", "acscourier": "ACS Courier", "adaexpress": "明大快递", "adapost": "安达速递", "adiexpress": "安达易国际速递", "adlerlogi": "德国雄鹰速递", "adp": "ADP国际快递", "adsone": "ADSone", "advancing": "安达信", "afghan": "阿富汗(Afghan Post)", "afl": "AFL", "agility": "Agility Logistics", "agopost": "全程快递", "ahdf": "德方物流", "ahkbps": "卡邦配送", "airgtc": "加拿大民航快递", "airpak": "airpak expresss", "ajexpress": "捷记方舟", "ajlogistics": "澳捷物流", "aland": "奥兰群岛", "albania": "阿尔巴尼亚(Posta shqipatre)", "alfatrex": "AlfaTrex", "algeria": "Algeria", "aliexpress": "无忧物流", "alliedexpress": "ALLIED", "alog": "心怡物流", "amazon_fba_swiship": "Swiship UK", "amazoncnorder": "亚马逊中国订单", "amcnorder": "amazon-国内订单", "amusorder": "amazon-国际订单", "ane66": "安能快递", "anjiatongcheng": "安家同城快运", "anjie88": "安捷物流", "anjiekuaidi": "青岛安捷快递", "anjun_logistics": "Anjun Logistics", "anlexpress": "新干线快递", "annengwuliu": "安能快运", "annto": "安得物流", "anposten": "爱尔兰(An Post)", "anshidi": "安时递", "anteraja": "Anteraja", "anxindakuaixi": "安信达", "anxl": "安迅物流", "aolau": "AOL澳通速递", "aosu": "澳速物流", "aotsd": "澳天速运", "apc": "APC Postal Logistics", "apgecommerce": "apgecommerce", "aplus100": "美国汉邦快递", "aramex": "Aramex", "arc": "ARC", "ariesfar": "艾瑞斯远", "arkexpress": "方舟速递", "aruba": "阿鲁巴[荷兰]（Post Aruba）", "asendia": "阿森迪亚", "asendiahk": "Asendia HK", "asendiausa": "Asendia USA", "astexpress": "安世通快递", "auadexpress": "澳达国际物流", "auex": "澳货通", "auexpress": "澳邮中国快运", "auod": "澳德物流", "ausbondexpress": "澳邦国际物流", "ausexpress": "澳世速递", "auspost": "澳大利亚(Australia Post)", "austa": "Austa国际速递", "austria": "奥地利(Austrian Post)", "auvanda": "中联速递", "auvexpress": "AUV国际快递", "axexpress": "澳新物流", "azerbaijan": "阿塞拜疆EMS(EMS AzerExpressPost)", "bahrain": "巴林(Bahrain Post)", "baifudongfang": "百福东方", "baishiguoji": "百世国际", "baishiwuliu": "百世快运", "baishiyp": "百世云配", "baitengwuliu": "百腾物流", "bangbangpost": "帮帮发", "bangladesh": "孟加拉国(EMS)", "bangsongwuliu": "邦送物流", "banma": "斑马物流", "baotongkd": "宝通快递", "baoxianda": "报通快递", "barbados": "巴巴多斯(Barbados Post)", "bazirim": "皮牙子快递", "bcwelt": "BCWELT", "bdatong": "八达通", "bdcgcc": "BDC快递", "beckygo": "佰麒快递", "bee001": "蜜蜂速递", "beebird": "锋鸟物流", "beeexpress": "BEE express", "beiou": "beiou express", "belgiumpost": "比利时(Belgium Post)", "belize": "伯利兹(Belize Postal)", "belpost": "白俄罗斯(Belpochta)", "benin": "贝宁", "benniao": "笨鸟国际", "benteng": "奔腾物流", "bester": "飛斯特", "betterlife": "東邮寄", "bflg": "上海缤纷物流", "bht": "BHT", "bhutan": "不丹邮政 Bhutan Post", "biaojikuaidi": "彪记快递", "bjemstckj": "北京EMS", "bjqywl": "青云物流", "bjxsrd": "鑫锐达", "bjytsywl": "远通盛源", "bljt56": "佰乐捷通", "bluedart": "BlueDart", "bmlchina": "标杆物流", "bmxps": "北美国际物流", "bohei": "波黑(JP BH Posta)", "bolivia": "玻利维亚", "borderguru": "BorderGuru", "bosind": "堡昕德速递", "bosta": "Bosta", "botspost": "博茨瓦纳", "boxc": "BOXC", "boyol": "贝业物流", "bphchina": "速方(Sufast)", "bpost": "比利时（Bpost）", "bpostinter": "比利时国际(Bpost international)", "bqcwl": "百千诚物流", "brazilposten": "巴西(Brazil Post/Correios)", "bridgeexpress ": "Bridge Express ", "briems": "宏桥国际物流", "brt": "BRT", "brunei": "文莱(Brunei Postal)", "bsht": "百事亨通", "btexpress": "邦泰快运", "bulgarian": "保加利亚（Bulgarian Posts）", "buyer1688": "1688国际物流", "buylogic": "Buylogic", "buytong": "百通物流", "byht": "展勤快递", "caledonia": "新喀里多尼亚[法国](New Caledonia)", "cambodia": "柬埔寨(Cambodia Post)", "camekong": "到了港", "cameroon": "喀麦隆(CAMPOST)", "campbellsexpress": "Campbell’s Express", "canhold": "能装能送", "canpar": "Canpar", "canpost": "加拿大(Canada Post)", "canpostfr": "加拿大邮政", "cargolux": "卢森堡航空", "cbl": "CBL Logistica", "cbl_logistica": "CBL Logistics", "cbllogistics": "广州信邦", "cbo56": "钏博物流", "cccc58": "中集冷云", "ccd": "次晨达物流", "cces": "CCES/国通快递", "cd3fwl": "中迅三方", "cdek": "CDEK", "cdjx56": "捷祥物流", "cdxinchen56": "鑫宸物流", "ceskaposta": "捷克（?eská po?ta）", "ceva": "CEVA Logistics", "cevalogistics": "CEVA Logistic", "cex": "城铁速递", "cfss": "银雁专送", "changjiang": "长江国际速递", "changwooair": "昌宇国际", "chengda": "成达国际速递", "chengji": "城际快递", "chengning": "城宁供应链", "chengpei": "河北橙配", "chengtong": "城通物流", "chile": "智利(Correos Chile)", "chinaicip": "卓志速运", "chinapost": "中国邮政（CHINA POST）", "chinapostcb": "中邮电商", "chinaqingguan": "荣通国际", "chinasqk": "SQK国际速递", "chinastarlogistics": "华欣物流", "chinatzx": "同舟行物流", "chllog": "嘉荣物流", "chnexp": "中翼国际物流", "chronopostfra": "法国大包、EMS-法文（Chronopost France）", "chronopostfren": "法国大包、EMS-英文(Chronopost France)", "chronopostport": "Chronopost Portugal", "chszhonghuanguoji": "CHS中环国际快递", "cht361": "诚和通", "chuangyi": "创一快递", "chuanxiwuliu": "传喜物流", "chukou1": "出口易", "chunfai": "中国香港骏辉物流", "chunghwa56": "中骅物流", "city56": "城市映急", "citylink": "City-Link", "citysprint": "citysprint", "cjkoreaexpress": "大韩通运", "cjqy": "长吉物流", "ckeex": "城晓国际快递", "ckexpress": "CK物流", "cllexpress": "澳通华人物流", "cloudexpress": "CE易欧通国际速递", "cloudlogistics365": "群航国际物流", "clsp": "CL日中速运", "cnair": "CNAIR", "cnausu": "中澳速递", "cncexp": "C\u0026C国际速递", "cneulogistics": "中欧物流", "cnexps": "CNE", "cnpex": "CNPEX中邮快递", "cnspeedster": "速舟物流", "cnup": "CNUP 中联邮", "cnws": "中国翼", "coe": "COE", "colissimo": "法国小包（colissimo）", "collectplus": "Collect+", "colombia": "哥伦比亚(4-72 La Red Postal de Colombia)", "com1express": "商壹国际物流", "comexpress": "邦通国际", "concare": "中健云康", "corporatecouriers": "Corporate couriers logistics", "correios": "莫桑比克（Correios de Moçambique）", "correo": "乌拉圭（Correo Uruguayo）", "correoargentino": "阿根廷(Correo Argentina)", "correos": "哥斯达黎加(Correos de Costa Rica)", "correosdees": "西班牙(Correos de Espa?a)", "correosexpress": "Correos Express", "cosco": "中远e环球", "courierpost": "CourierPost", "couriersplease": "Couriers Please", "cpsair": "华中快递", "cqxingcheng": "重庆星程快递", "crazyexpress": "疯狂快递", "crossbox": "环旅快运", "csuivi": "法国(La Poste)", "csxss": "新时速物流", "ctoexp": "泰国中通CTO", "cypruspost": "塞浦路斯(Cyprus Post)", "cyxfx": "小飞侠速递", "czwlyn": "云南诚中物流", "dachser": "DACHSER", "dadaoex": "大道物流", "dande56": "丹递56", "danniao": "丹鸟", "dasu": "达速物流", "datianwuliu": "大田物流", "dayangwuliu": "大洋物流", "dcs": "DCS", "ddotbase": "叮当同城", "debangkuaidi": "德邦快递", "debangwuliu": "德邦", "dechuangwuliu": "深圳德创物流", "decnlh": "德中快递", "dekuncn": "德坤物流", "delhivery": "Delhivery", "deliverystations": "Delivery Station", "deltec": "Deltec Courier", "deployeg": "Deploy", "desworks": "澳行快递", "deutschepost": "德国(Deutsche Post)", "dfglobalex": "东风全球速递", "dfpost": "达方物流", "dfwl": "达发物流", "dhl": "DHL-中国件", "dhlbenelux": "DHL Benelux", "dhlde": "DHL-德国件（DHL Deutschland）", "dhlecommerce": "dhl小包", "dhlen": "DHL-全球件", "dhlhk": "DHL HK", "dhlnetherlands": "DHL-荷兰（DHL Netherlands）", "dhlpoland": "DHL-波兰（DHL Poland）", "dhluk": "dhluk", "di5pll": "递五方云仓", "dianyi": "云南滇驿物流", "didasuyun": "递达速运", "dindon": "叮咚澳洲转运", "dingdong": "叮咚快递", "directlink": "Direct Link", "disifang": "递四方", "disifangau": "递四方澳洲", "disifangus": "递四方美国", "djibouti": "吉布提", "djy56": "天翔东捷运", "donghanwl": "东瀚物流", "donghong": "东红物流", "dongjun": "成都东骏物流", "doortodoor": "CJ物流", "dotzot": "Dotzot", "dpd": "DPD", "dpd_ireland": "DPD Ireland", "dpdgermany": "DPD Germany", "dpdpoland": "DPD Poland", "dpduk": "DPD UK", "dpe_express": "DPE Express", "dpex": "DPEX", "dpexen": "Toll", "dreevo": "dreevo", "driverfastgo": "老司机国际快递", "dsukuaidi": "D速快递", "dsv": "DSV", "dt8ang": "德淘邦", "dtd": "DTD", "dtdcindia": "DTDC India", "duodao56": "多道供应链", "dushisc": "渡石医药", "dyexpress": "大亿快递", "ealceair": "东方航空物流", "easyexpress": "EASY EXPRESS", "ecallturn": "E跨通", "ecfirstclass": "EC-Firstclass", "echo": "Echo", "ecmscn": "易客满", "ecmsglobal": "ECMS Express", "ecomexpress": "Ecom Express", "ecotransite": "东西E全运", "ecuador": "厄瓜多尔(Correos del Ecuador)", "edaeuexpress": "易达快运", "edragon": "龙象国际物流", "edtexpress": "e直运", "efs": "EFS Post（平安快递）", "efspost": "EFSPOST", "egyexpress": "EGY Express Logistics", "egypt": "埃及（Egypt Post）", "egyptexpress": "Egypt Express", "eiffel": "艾菲尔国际速递", "ekart": "Ekart", "el56": "易联通达", "elta": "希腊包裹（ELTA Hellenic Post）", "eltahell": "希腊EMS（ELTA Courier）", "emirates": "阿联酋(Emirates Post)", "emiratesen": "阿联酋(Emirates Post)", "emms": "澳州顺风快递", "emonitoring": "波兰小包(Poczta Polska)", "emonitoringen": "波兰小包(Poczta Polska)", "ems": "EMS", "emsbg": "EMS包裹", "emsen": "EMS-英文", "emsguoji": "EMS-国际件", "emsinten": "EMS-国际件-英文", "emsluqu": "高考通知书", "emssouthafrica": "南非EMS", "emsukraine": "乌克兰EMS(EMS Ukraine)", "emsukrainecn": "乌克兰EMS-中文(EMS Ukraine)", "emswuliu": "EMS物流", "england": "英国(大包,EMS)", "epanex": "泛捷国际速递", "epspost": "联众国际", "equick_cn": "Equick China", "eripostal": "厄立特里亚", "erqianjia56": "贰仟家物流", "eshunda": "俄顺达", "esinotrans": "中外运", "est365": "东方汇", "estafeta": "Estafeta", "estes": "Estes", "eta100": "易达国际速递", "eteenlog": "ETEEN专线", "ethiopia": "埃塞俄比亚(Ethiopian postal)", "ethiopian": "埃塞俄比亚(Ethiopian Post)", "etong": "E通速递", "euasia": "欧亚专线", "eucnrail": "中欧国际物流", "eucpost": "德国 EUC POST", "euexpress": "EU-EXPRESS", "euguoji": "易邮国际", "eupackage": "易优包裹", "europe8": "败欧洲", "europeanecom": "europeanecom", "eusacn": "优莎速运", "ewe": "EWE全球快递", "exbtr": "飛斯特運通", "excocotree": "可可树美中速运", "exfresh": "安鲜达", "expeditors": "Expeditors", "explorer56": "探路速运", "express2global": "E2G速递", "express7th": "7号速递", "expressplus": "澳洲新干线快递", "exsuda": "E速达", "ezhuanyuan": "易转运", "fandaguoji": "颿达国际快递-英文", "fanyukuaidi": "凡宇快递", "fardarww": "颿达国际快递", "farlogistis": "泛远国际物流", "fastgoexpress": "速派快递", "fastontime": "加拿大联通快运", "fastway": "Fastway Ireland", "fastway_nz": "Fastway New Zealand", "fastway_za": "Fastway South Africa", "fastzt": "正途供应链", "fbkd": "飞邦快递", "fds": "FDS", "fedex": "FedEx-国际件", "fedexcn": "Fedex-国际件-中文", "fedexuk": "FedEx-英国件（FedEx UK)", "fedexukcn": "FedEx-英国件", "fedexus": "FedEx-美国件", "fedroad": "FedRoad 联邦转运", "feibaokuaidi": "飞豹快递", "feihukuaidi": "飞狐快递", "feikangda": "飞康达", "feikuaida": "飞快达", "feiyuanvipshop": "飞远配送", "fenghuangkuaidi": "凤凰快递", "fengtianexpress": "奉天物流", "fengwang": "丰网速运", "fengyee": "丰羿", "fiji": "斐济(Fiji Post)", "finland": "芬兰(Itella Posti Oy)", "firstflight": "First Flight", "firstlogistics": "First Logistics", "flashexpress": "Flash Express", "flashexpressen": "Flash Express-英文", "flextock": "Flextock", "flowerkd": "花瓣转运", "flysman": "飞力士物流", "flyway": "程光快递", "fodel": "FODEL", "fourpxus": "四方格", "fox": "FOX国际快递", "freakyquick": "FQ狂派速递", "fsexp": "全速快递", "ftd": "富腾达国际货运", "ftky365": "丰通快运", "ftlexpress": "法翔速运", "fujisuyun": "富吉速运", "fyex": "飞云快递系统", "ganzhongnengda": "能达速递", "gaotieex": "高铁快运", "gaticn": "Gati-中文", "gatien": "Gati-英文", "gatikwe": "Gati-KWE", "gda": "安的快递", "gdct56": "广东诚通物流", "gdex": "GDEX", "gdkjk56": "快捷快物流", "gdqwwl": "全网物流", "gdrz58": "容智快运", "gdxp": "新鹏快递", "ge2d": "GE2D跨境物流", "georgianpost": "格鲁吉亚(Georgian Pos）", "gexpress": "G EXpress", "gfdwuliu": "冠达丰物流", "ghanapost": "加纳", "ghl": "环创物流", "ghtexpress": "GHT物流", "gibraltar": "直布罗陀[英国]( Royal Gibraltar Post)", "giztix": "GIZTIX", "gjwl": "冠捷物流 ", "global99": "全球速运", "globaltracktrace": "globaltracktrace", "gls": "GLS", "gls_italy": "GLS Italy", "gml": "英脉物流", "goex": "时安达速递", "gogox": "GOGOX", "gojavas": "gojavas", "goldjet": "高捷快运", "gongsuda": "共速达", "gooday365": "日日顺智慧物联", "gotoubi": "UBI Australia", "grab": "Grab", "greenland": "格陵兰[丹麦]（TELE Greenland A/S）", "grivertek": "潍鸿", "gscq365": "哥士传奇速递", "gslexpress": "德尚国际速递", "gslhkd": "联合快递", "gsm": "GSM", "gswtkd": "万通快递", "gtgogo": "GT国际快运", "gts": "GTS快递", "gttexpress": "GTT EXPRESS快递", "guangdongtonglu": "广东通路", "guangdongyongbang": "永邦快递", "guangdongyouzhengwuliu": "广东邮政", "guanting": "冠庭国际物流", "guernsey": "根西岛", "guexp": "全联速运", "guoeryue": "天天快物流", "guoshunda": "国顺达物流", "guosong": "国送快运", "guotongkuaidi": "国通快递", "guyana": "圭亚那", "gvpexpress": "宏观国际快递", "gxwl": "光线速递", "gzanjcwl": "广州安能聚创物流", "gzxingcheng": "贵州星程快递", "h66": "货六六", "hac56": "瀚朝物流", "haidaibao": "海带宝", "haihongmmb": "海红for买卖宝", "haihongwangsong": "海红网送", "haimengsudi": "海盟速递", "haiwaihuanqiu": "海外环球", "haixingqiao": "海星桥快递", "haizhongzhuanyun": "海中转运", "handboy": "汉邦国际速递", "hanfengjl": "翰丰快递", "hangrui": "上海航瑞货运", "hangyu": "航宇快递", "hanxin": "汉信快递", "haoxiangwuliu": "豪翔物流", "haoyoukuai": "好又快物流", "happylink": "开心快递", "haypost": "亚美尼亚(Haypost-Armenian Postal)", "hd": "宏递快运", "hdcexpress": "汇达物流", "heibaowuliu": "黑豹物流", "heimao56": "黑猫速运", "hengluwuliu": "恒路物流", "hengrui56": "恒瑞物流", "hermes": "Hermes", "hermes_de": "Hermes Germany", "hermesworld": "Hermesworld", "hexinexpress": "合心速递", "hgy56": "环国运物流", "hhair56": "华瀚快递", "highsince": "海欣斯快递", "highway": "Highway", "hitaoe": "Hi淘易快递", "hjs": "猴急送", "hjwl": "汇捷物流", "hkeex": "飞豹速递", "hkems": "云邮跨境快递", "hkpost": "中国香港(HongKong Post)", "hkposten": "中国香港(HongKong Post)英文", "hlkytj": "互联快运", "hlpgyl": "共联配", "hltop": "海联快递", "hlyex": "好来运", "hmus": "华美快递", "hnfy": "飞鹰物流", "hnht56": "鸿泰物流", "hnqst": "河南全速通", "hnssd56": "顺时达物流", "hnzqwl": "中强物流", "holisollogistics": "Holisol", "homecourier": "如家国际快递", "homexpress": "居家通", "honduras": "洪都拉斯", "hongbeixin": "红背心", "hongjie": "宏捷国际物流", "hongpinwuliu": "宏品物流", "hongywl": "红远物流", "hotwms": "皇家云仓", "hpexpress": "海派国际速递", "hqtd": "环球通达 ", "hrbsydrd": "速远同城快递", "hrbzykd": "卓烨快递", "hre": "高铁速递", "hrex": "锦程快递", "hrvatska": "克罗地亚（Hrvatska Posta）", "hsdexpress": "寰世达国际物流", "hsgtsd": "海硕高铁速递", "ht22": "海淘物流", "htongexpress": "华通快运", "httx56": "汇通天下物流", "htwd": "华通务达物流", "huada": "华达快运", "huahanwuliu": "华翰物流", "huandonglg": "环东物流", "huangmajia": "黄马甲", "huanqiu": "环球速运", "huanqiuabc": "中国香港环球快运", "huaqikuaiyun": "华企快运", "huaxiahuoyun": "华夏货运", "huif56": "汇峰物流", "huilin56": "汇霖大货网", "huiqiangkuaidi": "汇强快递", "huisenky": "汇森速运", "huitongkuaidi": "百世快递", "humpline": "驼峰国际", "hungary": "匈牙利（Magyar Posta）", "hunterexpress": "hunter Express", "huoban": "兰州伙伴物流", "huolalawuliu": "货拉拉物流", "hutongwuliu": "户通物流", "hyeship": "鸿远物流", "hyk": "上海昊宏国际货物", "hywuliu": "中电华远物流", "hyytes": "恒宇运通", "hzpl": "华航快递", "ibcourier": "IB Courier", "ibuy8": "爱拜物流", "iceland": "冰岛(Iceland Post)", "idada": "大达物流", "idamalu": "大马鹿", "idexpress": "ID Express", "iex": "泛太优达", "iexpress": "iExpress", "igcaexpress": "无限速递", "ilogen": "logen路坚", "ilyang": "ILYANG", "imile": "iMile", "imlb2c": "艾姆勒", "india": "印度(India Post)", "indonesia": "印度尼西亚EMS(Pos Indonesia-EMS)", "indopaket": "INDOPAKET", "inposdom": "多米尼加（INPOSDOM – Instituto Postal Dominicano）", "inpost_paczkomaty": "InPost Paczkomaty", "interjz": "捷运达快递", "interlink": "Interlink Express", "interparcel": "Interparcel", "iparcel": "UPS i-parcel", "iran": "伊朗（Iran Post）", "israelpost": "以色列(Israel Post)", "italiane": "意大利(Poste Italiane)", "italysad": "Italy SDA", "ixpress": "IXPRESS", "iyoungspeed": "驿扬国际速运", "jamaicapost": "牙买加（Jamaica Post）", "janio": "janio", "japanpost": "日本郵便", "japanposten": "日本（Japan Post）", "jcex": "jcex", "jcsuda": "嘉诚速达", "jd": "京东物流", "jdexpressusa": "骏达快递", "jdiex": "JDIEX", "jdpplus": "急递", "jerseypost": "泽西岛", "jet": "极兔国际", "jetexpresseg": "Jet Express", "jetexpressgroup": "澳速通国际速递", "jetexpresszh": "J\u0026T Express", "jetstarexp": "捷仕", "jgwl": "景光物流", "jiachenexpress": "佳辰国际速递", "jiacheng": "佳成快递 ", "jiajiatong56": "佳家通货运", "jiajiawl": "加佳物流", "jiajikuaidi": "佳吉快递", "jiajiwuliu": "佳吉快运", "jialidatong": "嘉里大通", "jiaxianwuliu": "嘉贤物流", "jiayiwuliu": "佳怡物流", "jiayunmeiwuliu": "加运美", "jiazhoumao": "加州猫速递", "jieanda": "捷安达", "jieborne": "捷邦物流", "jiguang": "极光转运", "jinan": "金岸物流", "jinchengwuliu": "锦程物流", "jindawuliu": "金大物流", "jinduan": "近端", "jingdongkuaiyun": "京东快运", "jingshun": "景顺物流", "jinguangsudikuaijian": "京广速递", "jintongkd": "劲通快递", "jinyuekuaidi": "晋越快递", "jisu": "冀速物流", "jiugong": "九宫物流", "jiujiuwl": "久久物流", "jiuyescm": "九曳供应链", "jiuyicn": "久易快递", "jixianda": "急先达", "jixiangyouau": "吉祥邮（澳洲）", "jjx888": "佳捷翔物流", "jne": "JNE", "jordan": "约旦(Jordan Post)", "jsexpress": "骏绅物流", "jssdt56": "时达通", "jtexpress": "极兔速递", "jtexpresseg": "极兔快递埃及站", "jtexpressmy": "J\u0026T Express 马来西亚", "jtexpressph": "J\u0026T Express 菲律宾", "jtexpresssg": "J\u0026T Express 新加坡", "jtexpressth": "J\u0026T Express 泰国", "juding": "聚鼎物流", "jumia": "Jumia", "jumstc": "聚盟共建", "junfengguoji": "骏丰国际速递", "juwu": "聚物物流", "juzhongda": "聚中大", "jxfex": "集先锋快递", "kahaexpress": "Kaha Epress", "kangaroo": "Kangaroo Express", "kaolaexpress": "考拉国际速递", "kazpost": "哈萨克斯坦(Kazpost)", "kcs": "KCS", "kejie": "科捷物流", "kenya": "肯尼亚(POSTA KENYA)", "kerrythailand": "Kerry Express-泰国", "kerrythailanden": "Kerry Express-泰国", "kerrytj": "嘉里大荣物流", "keypon": "启邦国际物流", "kfwnet": "快服务", "khzto": "柬埔寨中通", "kingfreight": "货运皇", "kjde": "跨境直邮通", "kn": "Kuehne + Nagel", "koalaexp": "考拉速递", "koali": "番薯国际货运", "koreapost": "韩国（Korea Post）", "koreapostcn": "韩国邮政", "koreapostkr": "韩国邮政韩文", "krtao": "淘韩国际快递", "ksudi": "快速递", "kuai8": "快8速运", "kuaidawuliu": "快达物流", "kuaijiesudi": "快捷速递", "kuaijiewuliu": "快捷物流", "kuaika": "快卡", "kuaitao": "快淘快递", "kuaiyouda": "四川快优达速递", "kuayue": "跨越速运", "kuehnenagel": "Kuehne+Nagel", "kurasi": "KURASI", "kxda": "凯信达", "kyrgyzpost": "吉尔吉斯斯坦(Kyrgyz Post)", "kyue": "跨跃国际", "la911": "鼎润物流", "lahuoex": "拉火速运", "lanbiaokuaidi": "蓝镖快递", "landmarkglobal": "Landmark Global", "lanhukuaidi": "蓝弧快递", "lao": "老挝(Lao Express) ", "laposte": "塞内加尔", "lasership": "LaserShip", "lasy56": "林安物流", "latvia": "拉脱维亚(Latvijas Pasts)", "latviaen": "拉脱维亚(Latvijas Pasts)", "lbbk": "立白宝凯物流", "ldxpres": "林道国际快递-英文", "ldzy168": "两点之间", "ledaexpress": "乐达全球速递", "ledaowuliu": "楽道物流", "ledii": "乐递供应链", "leopard": "云豹国际货运", "leshines": "想乐送", "lesotho": "莱索托(Lesotho Post)", "letseml": "美联快递", "lexship": "Laxship", "lfexpress": "龙枫国际快递", "lgs": "lazada", "lhexpressus": "联合速递", "lianbangkuaidi": "联邦快递", "lianbangkuaidien": "联邦快递-英文", "lianhaowuliu": "联昊通", "lianyun": "联运快递", "libanpost": "黎巴嫩(Liban Post)", "lijisong": "成都立即送", "lineone": "一号线", "linex": "Linex", "lingsong": "领送送", "lionparcel": "Lion Parcel", "lishi": "丽狮物流", "lithuania": "立陶宛（Lietuvos pa?tas）", "littlebearbear": "小熊物流", "liujiashen": "润禾物流", "lizhan": "力展物流", "lmfex": "良藤国际速递", "lnet": "新易泰", "lntjs": "特急送", "loadbugs": "Loadbugs", "logistics": "華信物流WTO", "longcps": "加拿大龙行速运", "longfx": "LUCFLOW EXPRESS", "longlangkuaidi": "隆浪快递", "longvast": "长风物流", "loomisexpress": "Loomis Express", "lqht": "恒通快递", "lsexpress": "6LS EXPRESS", "ltexp": "乐天速递", "ltparcel": "联通快递", "lubang56": "路邦物流", "luben": "陆本速递 LUBEN EXPRESS", "luckyfastex": "吉捷国际速递", "lundao": "论道国际物流", "lutong": "鲁通快运", "luxembourg": "卢森堡(Luxembourg Post)", "luxembourgde": "卢森堡(Luxembourg Post)", "luxembourgfr": "卢森堡(Luxembourg Post)", "luyao": "路遥物流", "luzun": "路尊物流", "lwe": "LWE", "macao": "中国澳门(Macau Post)", "macedonia": "马其顿(Macedonian Post)", "maersk": "Maersk", "mailamericas": "mailamericas", "mailikuaidi": "麦力快递", "mailongdy": "迈隆递运", "mainfreight": "Mainfreight", "malaysiaems": "马来西亚大包、EMS（Malaysia Post(parcel,EMS)）", "malaysiapost": "马来西亚小包（Malaysia Post(Registered)）", "maldives": "马尔代夫(Maldives Post)", "malta": "马耳他（Malta Post）", "mangguo": "芒果速递", "manwah": "敏華物流", "mapleexpress": "今枫国际快运", "mascourierservice": "MAS Courier Servic", "matkahuolto": "Matkahuolto", "mauritius": "毛里求斯(Mauritius Post)", "maxeedexpress": "澳洲迈速快递", "mchy": "木春货运", "mecs": "Middle East Courier service", "meibang": "美邦国际快递", "meidaexpress": "美达快递", "meiguokuaidi": "美国快递", "meiquick": "美快国际物流", "meitai": "美泰物流", "meixi": "美西快递", "merage": "Merage ", "mexico": "墨西哥（Correos de Mexico）", "mexicodenda": "Mexico Senda Express", "mhsy": "名航速运", "milkyway": "银河物流", "minbangsudi": "民邦速递", "minghangkuaidi": "民航快递", "mingliangwuliu": "明亮物流", "mjexp": "美龙快递", "mlw": "美乐维冷链物流", "mmlogi": "猛犸速递", "mol": "Mitsui OSK Lines", "moldova": "摩尔多瓦(Posta Moldovei)", "mongolpost": "蒙古国(Mongol Post) ", "montenegro": "黑山(Posta Crne Gore)", "morelink56": "MoreLink", "morocco": "摩洛哥 ( Morocco Post )", "mosuda": "魔速达", "mrw": "MRW", "multipack": "Mexico Multipack", "mxe56": "中俄速通（淼信）", "myhermes": "MyHermes", "mylerz": "mylerz", "nalexpress": "新亚物流", "namibia": "纳米比亚(NamPost)", "nandan": "NandanCourier", "nanjingshengbang": "晟邦物流", "naqel": "NAQEL Express", "nationex": "Nationex", "nbhtt": "早道佳", "ndwl": "南方传媒物流", "nebuex": "星云速递", "nedahm": "红马速递", "nederlandpost": "荷兰速递(Nederland Post)", "nell": "尼尔快递", "nepalpost": "尼泊尔（Nepal Postal Services）", "networkcourier": "Network Courier", "newgistics": "Newgistics", "newsway": "家家通快递", "newzealand": "新西兰（New Zealand Post）", "nezhasuyun": "哪吒速运", "nigerianpost": "尼日利亚(Nigerian Postal)", "ninjavan": "Ninja Van ", "ninjaxpress": "ninja xpress", "nipponexpress": "Nippon Express", "niuzaiexpress": "牛仔速运", "njhaobo": "浩博物流", "nle": "NLE", "nlebv": "亚欧专线", "nmhuahe": "华赫物流", "nntengda": "腾达速递", "nokcourier": "Nok Courier", "norsk_global": "Norsk Global", "novaposhta": "Nova Poshta", "nsf": "新顺丰（NSF）", "nuoer": "诺尔国际物流", "nuoyaao": "偌亚奥国际快递", "nyk": "NYK Line", "nzzto": "新西兰中通", "ocaargen": "OCA Argentina", "ocpost": "OC-Post", "ocs": "OCS", "ocsindia": "OCS ANA Group", "oman": "阿曼(Oman Post)", "omni2": "无限配", "omniparcel": "Omni Parcel", "omniva": "爱沙尼亚(Eesti Post)", "oneexpress": "一速递", "onehcang": "一号仓", "oneworldexpress": "One World Express", "ontrac": "OnTrac", "onway": "昂威物流", "opek": "OPEK", "opex": "OPEX", "overseaex": "波音速递", "packlink": "Packlink", "pakistan": "巴基斯坦(Pakistan Post)", "panama": "巴拿马", "pandulogistics": "Pandu Logistics", "paraguay": "巴拉圭(Correo Paraguayo)", "parcel2go": "parcel2go", "parcelchina": "诚一物流", "parcelforce": "英国大包、EMS（Parcel Force）", "parcelforcecn": "英国邮政大包EMS", "parknparcel": "Park N Pracel", "passerbyaexpress": "顺捷美中速递", "paxel": "paxel", "paxelen": "paxelen", "pcaexpress": "PCA Express", "pcwl56": "普畅物流", "pdstow": "全球速递", "peex": "派尔快递", "peisihuoyunkuaidi": "配思货运", "peixingwuliu": "陪行物流", "pengcheng": "鹏程快递", "pengyuanexpress": "鹏远国际速递", "perfectservice": "Perfect Service", "peru": "秘鲁(SERPOST)", "pesto": "Presto", "pfcexpress": "皇家物流", "pflogistics": "Parcel Freight Logistics", "phlpost": "菲律宾（Philippine Postal）", "pickupp": "pickupp", "pingandatengfei": "平安达腾飞", "pinsuxinda": "品速心达快递", "pinxinkuaidi": "品信快递", "pioneer": "先锋国际快递", "pjbest": "品骏快递", "pmt0704be": "龙行天下", "pochta": "俄罗斯邮政(Russian Post)", "polarexpress": "极地快递", "polarisexpress": "北极星快运", "portugalctt": "葡萄牙（Portugal CTT）", "portugalctten": "葡萄牙（Portugal CTT）", "portugalseur": "Portugal Seur", "posindonesia": "POS INDONESIA", "posta": "坦桑尼亚（Tanzania Posts Corporation）", "postdanmarken": "丹麦(Post Denmark)", "postelbe": "PostElbe", "postenab": "PostNord(Posten AB)", "postennorge": "挪威（Posten Norge）", "postnl": "荷兰邮政(PostNL international registered mail)", "postnlchina": "荷兰邮政-中国件", "postnlcn": "荷兰邮政-中文(PostNL international registered mail)", "postnlpacle": "荷兰包裹(PostNL International Parcels)", "postnord": "PostNord Logistics", "postpng": "巴布亚新几内亚(PNG Post)", "postserv": "中华邮政", "primamulticipta": "PT Prima Multi Cipta", "ptt": "土耳其", "purolator": "Purolator", "pzhjst": "急顺通", "qbexpress": "秦邦快运", "qdants": "ANTS EXPRESS", "qesd": "7E速递", "qexpress": "易达通快递", "qhxykd": "雪域快递", "qhxyyg": "雪域易购", "qianli": "千里速递", "qichen": "启辰国际速递", "qinling": "秦岭智能速运", "qinyuan": "秦远物流", "qpost": "卡塔尔（Qatar Post）", "qskdyxgs": "千顺快递", "quanchuan56": "全川摩运", "quanfengkuaidi": "全峰快递", "quanjitong": "全际通", "quanritongkuaidi": "全日通", "quansu": "全速物流", "quansutong": "全速通", "quantium": "Quantium", "quantwl": "全通快运", "quanxintong": "全信通快递", "quanyikuaidi": "全一快递", "qxpress": "Qxpress", "qzx56": "全之鑫物流", "r2slogistics": "R2S Logistics", "ramgroup_za": "RAM", "rapidocargoeg": "Rapido Cargo Egypt", "redexpress": "Red Express", "redur": "Redur", "redur_es": "Redur Spain", "renrenex": "人人转运", "republic": "叙利亚(Syrian Post)", "rhtexpress": "睿和泰速运", "riyuwuliu": "日昱物流", "rl_carriers": "RL Carriers", "rlgaus": "澳洲飞跃物流", "rokin": "荣庆物流", "romanian": "罗马尼亚（Posta Romanian）", "royal": "皇家国际速运", "rpx": "rpx", "rrs": "日日顺物流", "rrskx": "日日顺快线", "rrthk": "日日通国际", "ruidianyouzheng": "瑞典（Sweden Post）", "ruidianyouzhengen": "瑞典（Sweden Post）", "runbail": "润百特货", "runhengfeng": "全时速运", "rwanda": "卢旺达(Rwanda i-posita)", "s2c": "S2C", "safexpress": "Safexpress", "sagawa": "佐川急便", "sagawaen": "佐川急便-英文", "saiaodi": "赛澳递", "saiaodimmb": "赛澳递for买卖宝", "salvador": "Correo El Salvador", "samoa": "萨摩亚(Samoa Post)", "sanhuwuliu": "叁虎物流", "sanshengco": "三盛快递", "santaisudi": "三态速递", "sanzhi56": "三志物流", "sapexpress": "SAP EXPRESS", "saudipost": "沙特阿拉伯(Saudi Post)", "savor": "海信物流", "sccod": "丰程物流", "sccs": "SCCS", "scglogistics": "SCG", "scic": "中加国际快递", "scsujiada": "速佳达快运", "scxingcheng": "四川星程快递", "sczpds": "速呈", "sd138": "泰国138国际物流", "sdsy888": "首达速运", "sdto": "速达通", "selektvracht": "Selektvracht", "sendinglog": "森鼎国际物流", "sendle": "Sendle", "sendtochina": "速递中国", "serbia": "塞尔维亚(PE Post of Serbia)", "seur": "International Seur", "sfau": "澳丰速递", "sfc": "SFC", "sfcservice": "SFC Service", "sfift": "十方通物流", "sfjhd": "圣飞捷快递", "sfpost": "曹操到", "sfwl": "盛丰物流", "shadowfax": "Shadowfax", "shallexp": "穗航物流", "shanda56": "衫达快运", "shangcheng": "尚橙物流", "shangda": "上大物流", "shanghaikuaitong": "上海快通", "shanghaiwujiangmmb": "上海无疆for买卖宝", "shangqiao56": "商桥物流", "shangtuguoji": "尚途国际货运", "shanhuodidi": "闪货极速达", "shanxijianhua": "山西建华", "shaoke": "捎客物流", "shbwch": "杰响物流", "shd56": "商海德物流", "shenganwuliu": "圣安物流", "shenghuiwuliu": "盛辉物流", "shengtongscm": "盛通快递", "shenjun": "神骏物流", "shenma": "神马快递", "shentong": "申通快递", "shiligyl": "实利配送", "shiningexpress": "阳光快递", "shipblu": "ShipBlu", "shipbyace": "王牌快递", "shipgce": "飞洋快递", "shipsoho": "苏豪快递", "shiyunkuaidi": "世运快递", "shlexp": "SHL畅灵国际物流", "shlindao": "林道国际快递", "shpost": "同城快寄", "shpostwish": "wish邮", "shreeanjanicourier": "shreeanjanicourier", "shuanghe": "双鹤物流", "shunbang": "顺邦国际物流", "shunchangguoji": "顺昌国际", "shunfeng": "顺丰速运", "shunfenghk": "顺丰-繁体", "shunfengkuaiyun": "顺丰快运", "shunfenglengyun": "顺丰冷链", "shunfengnl": "顺丰-荷兰", "shunjieda": "顺捷达", "shunjiefengda": "顺捷丰达", "shunshid": "顺士达速运", "sicepat": "SiCepat Ekspres", "signedexpress": "签收快递", "sihaiet": "四海快递", "sihiexpress": "四海捷运", "singpost": "新加坡小包(Singapore Post)", "sinoairinex": "中外运空运", "sinoex": "中外运速递-中文", "siodemka": "Siodemka", "sixroad": "易普递", "sja56": "四季安物流", "skyexpresseg": "Sky Express", "skynet": "skynet", "skynetmalaysia": "SkyNet Malaysia", "skynetworldwide": "skynetworldwide", "skypost": "荷兰Sky Post", "skypostal": "Asendia HK (LATAM)", "slovak": "斯洛伐克(Slovenská Posta)", "slovenia": "斯洛文尼亚(Slovenia Post)", "slpost": "斯里兰卡(Sri Lanka Post)", "smsaexpress": "SMSA Express", "sofast56": "嗖一下同城快递", "solomon": "所罗门群岛", "sonapost": "布基纳法索", "southafrican": "南非（South African Post Office）", "speeda": "行必达", "speedaf": "Speedaf", "speedegypt": " Speed Shipping Company", "speedex": "SpeedEx", "speedoex": "申必达", "speedpost": "新加坡EMS、大包(Singapore Speedpost)", "spoton": "Spoton", "spring56": "春风物流", "sprint": "Sprint", "ssd": "速速达", "staky": "首通快运", "starex": "星速递", "staryvr": "星运快递", "stkd": "顺通快递", "stlucia": "圣卢西亚", "stoexpress": "美国申通", "stonewzealand": "申通新西兰", "stosolution": "申通国际", "stzd56": "智德物流", "subaoex": "速豹", "subida": "速必达", "sucheng": "速呈宅配", "sucmj": "特急便物流", "sudapost": "苏丹（Sudapost）", "suer": "速尔快递", "sufengkuaidi": "速风快递", "suijiawuliu": "穗佳物流", "sujievip": "郑州速捷", "sundarexpress": "顺达快递", "suning": "苏宁物流", "sunjex": "新杰物流", "sunspeedy": "新速航", "superb": "Superb Express", "superoz": "速配欧翼", "supinexpress": "速品快递", "surpost": "苏里南", "sut56": "速通物流", "suteng": "速腾快递", "sutonghongda": "速通鸿达", "sutongst": "速通国际快运", "suyoda": "速邮达", "svgpost": "圣文森特和格林纳丁斯", "swaziland": "斯威士兰", "swiship": "Amazon FBA Swiship", "swisspost": "瑞士(Swiss Post)", "swisspostcn": "瑞士邮政", "sxexpress": "三象速递", "sxhongmajia": "红马甲物流", "sxjdfreight": "顺心捷达", "synship": "SYNSHIP快递", "sypost": "顺友物流", "szdpex": "深圳DPEX", "szshihuatong56": "世华通物流", "szuem": "联运通物流", "szyouzheng": "深圳邮政", "szzss": "中时顺物流", "taijin": "泰进物流", "taimek": "天美快递", "takesend": "泰嘉物流", "talabat": "Talabat ", "tanzania": "坦桑尼亚(Tanzania Posts)", "taoplus": "淘布斯国际物流", "taote": "淘特物流快递", "tcat": "黑猫宅急便", "tcixps": "TCI XPS", "tcxbthai": "GPI", "tdcargo": "TD Cargo", "thailand": "泰国邮政（Thailand Thai Post）", "thebluebhellcouriers": "The BlueBhell Couriers", "thecourierguy": "The Courier Guy", "thunderexpress": "加拿大雷霆快递", "tiandihuayu": "天地华宇", "tianma": "天马迅达", "tiantian": "天天快递", "tianxiang": "天翔快递", "tianzong": "天纵物流", "tiki": "TiKi", "timedg": "万家通快递", "timelytitan": "Titan泰坦国际速递", "tipsa": "TIPSA", "tirupati": "Shree Tirupati", "tjkjwl": "泰实货运", "tjlyz56": "老扬州物流", "tlky": "天联快运", "tmg": "株式会社T.M.G", "tmwexpress": "明达国际速递", "tnjex": "明通国际快递", "tnt": "TNT", "tntau": "TNT Australia", "tnten": "TNT-全球件", "tntitaly": "TNT Italy", "tntpostcn": "TNT Post", "tntuk": "TNT UK", "tny": "TNY物流", "togo": "多哥", "tollpriority": "Toll Priority(Toll Online)", "tongdaxing": "通达兴物流", "tonghetianxia": "通和天下", "topshey": "顶世国际物流", "topspeedex": "中运全速", "tpcindia": "The Professional Couriers", "trackparcel": "track-parcel", "trakpak": "TRAKPAK", "transkargologistics": "Trans Kargo", "transporter": "Transporter Egypt", "transrush": "TransRush", "tstexp": "TST速运通", "ttkeurope": "天天欧洲物流", "tunisia": "突尼斯EMS(Rapid-Poste)", "turbo": "Turbo", "turtle": "海龟国际快递", "tykd": "天翼快递", "tywl99": "天翼物流", "tzky": "铁中快运", "ubonex": "优邦速运", "ubuy": "德国优拜物流", "ubx": "UBX", "ucs": "合众速递(UCS）", "udalogistic": "韵达国际", "ueq": "UEQ快递", "uex": "UEX国际物流", "uexiex": "欧洲UEX", "ufelix": "Ufelix", "uganda": "乌干达(Posta Uganda)", "ugoexpress": "邮鸽速运", "uhi": "优海国际速递", "ukraine": "乌克兰小包、大包(UkrPoshta)", "ukrpost": "乌克兰小包、大包(UkrPost)", "ukrpostcn": "乌克兰邮政包裹", "uluckex": "优联吉运", "uniexpress": "Uni Express", "unioncourier": "Union Courier SAE", "unitedex": "联合速运", "unitedexpress": "United express courier service", "uparcel": "UParcel", "ups": "UPS", "upsen": "UPS-全球件", "upsfreight": "UPS Freight", "upsmailinno": "UPS Mail Innovations", "usa7ex": "美七国际快递", "usasueexpress": "速翼快递", "uscbexpress": "易境达国际物流", "uschuaxia": "华夏国际速递", "usps": "USPS", "uspscn": "USPSCN", "uszcn": "转运中国", "utaoscm": "UTAO优到", "uzbekistan": "乌兹别克斯坦(Post of Uzbekistan)", "valueway": "美通", "vangenexpress": "万庚国际速递", "vanuatu": "瓦努阿图(Vanuatu Post)", "vctrans": "越中国际物流", "vietnam": "越南小包(Vietnam Posts)", "vipexpress": "鹰运国际速递", "vnpost": "越南EMS(VNPost Express)", "voo": "Voo", "vps": "维普恩物流", "wadily": "Wadily", "wahana": "Wahana", "wanboex": "万博快递", "wandougongzhu": "豌豆物流", "wanjiatong": "宁夏万家通", "wanjiawuliu": "万家物流", "wanxiangwuliu": "万象物流", "wassalnow": "WassalNow", "wdm": "万达美", "wedel": "Wedel", "wedepot": "wedepot物流", "weitepai": "微特派", "welogistics": "世航通运", "wenjiesudi": "文捷航空", "westwing": "西翼物流", "wexpress": "威速递", "wherexpess": "威盛快递", "whgjkd": "香港伟豪国际物流", "whistl": "Whistl", "winit": "万邑通", "wjkwl": "万家康物流", "wlfast": "凡仕特物流", "wln": "万理诺物流", "wlwex": "星空国际", "wondersyd": "中邮速递", "worldex": "世通物流", "wotu": "渥途国际速运", "wowexpress": "wowexpress", "wowvip": "沃埃家", "wtdchina": "威时沛运货运", "wtdex": "WTD海外通", "wto56kj": "臣邦同城", "wuliuky": "五六快运", "wuyuansudi": "伍圆速递", "wygj168": "万运国际快递", "wykjt": "51跨境通", "wzhaunyun": "微转运", "xdexpress": "迅达速递", "xdshipping": "国晶物流", "xflt56": "蓝天物流", "xhf56": "鑫宏福物流", "xianchengliansudi": "西安城联速递", "xiangdawuliu": "湘达物流", "xianglongyuntong": "祥龙运通物流", "xiangteng": "翔腾物流", "xilaikd": "西安喜来快递", "xinfengwuliu": "信丰物流", "xingyuankuaidi": "新元快递", "xinmengcheng": "鑫梦成", "xinning": "新宁物流", "xinyan": "新颜物流", "xiongda": "雄达国际物流", "xipost": "西邮寄", "xiyoug": "西游寄", "xjdaishu": "袋鼠速递", "xlair": "快弟来了", "xlobo": "Xlobo贝海国际", "xpertdelivery": "Xpert Delivery", "xpressbees": "XpressBees", "xsrd": "鑫世锐达", "xtb": "鑫通宝物流", "xunsuexpress": "迅速快递", "xunxuan": "迅选物流", "xyb2b": "行云物流", "xyd666": "鑫远东速运", "xyjexpress": "西游寄速递", "xynyc": "新元国际", "yafengsudi": "亚风速递", "yamaxunwuliu": "亚马逊中国", "yangbaoguo": "洋包裹", "yaofeikuaidi": "耀飞同城快递", "yaoqi": "耀奇物流", "yatfai": "一辉物流", "ycgglobal": "YCG物流", "ycgky": "远成快运", "ydfexpress": "易达丰国际速递", "ydglobe": "云达通", "ydhex": "YDH", "yemen": "也门(Yemen Post)", "yhtlogistics": "宇航通物流", "yibangwuliu": "一邦速递", "yidatong": "易达通", "yidihui": "驿递汇速递", "yiex": "宜送物流", "yifankd": "艺凡快递", "yihangmall": "易航物流", "yijiangky": "驿将快运", "yikonn": "yikonn", "yilingsuyun": "亿领速运", "yimidida": "壹米滴答", "yingchao": "英超物流", "yinjiesudi": "银捷速递", "yiouzhou": "易欧洲国际物流", "yiqiguojiwuliu": "一柒国际物流", "yiqisong": "一起送", "yishunhang": "亿顺航", "yisong": "宜送", "yitongda": "易通达", "yiyou": "易邮速运", "yizhengdasuyun": "一正达速运", "yjhgo": "武汉优进汇", "yjs": "益加盛快递", "ykouan": "洋口岸", "ynztsy": "黑猫同城送", "yodel": "YODEL", "yongbangwuliu": "永邦国际物流", "yongchangwuliu": "永昌物流", "yoseus": "优胜国际速递", "youban": "邮邦国际", "youjia": "友家速递", "youlai": "邮来速递", "yourscm": "雅澳物流", "youshuwuliu": "优速快递", "yousutongda": "优速通达", "youyibang": "邮驿帮高铁速运", "youyou": "优优速递", "youzhengbk": "邮政标准快递", "youzhengguoji": "国际包裹", "youzhengguonei": "邮政快递包裹", "ypsd": "壹品速递", "ysexpress": "鼹鼠快送", "ytchengnuoda": "承诺达", "ytkd": "运通中港快递", "ytky168": "运通快运", "yuananda": "源安达", "yuanchengwuliu": "远成物流", "yuandun": "远盾物流", "yuanfeihangwuliu": "原飞航", "yuanhhk": "远航国际快运", "yuantong": "圆通速递", "yuantongguoji": "圆通国际", "yuanzhijiecheng": "元智捷诚", "yue777": "玥玛速运", "yuefengwuliu": "越丰物流", "yuejiutong": "粤九通物流", "yufeng": "御风速运", "yujiawl": "宇佳物流", "yujtong": "宇捷通", "yunda": "韵达快递", "yundaexus": "美国云达", "yundakuaiyun": "韵达快运", "yunfeng56": "韵丰物流", "yunguo56": "蕴国物流", "yuntong": "运通速运", "yuntongkuaidi": "运通中港", "yuntrack": "YunExpress", "yusen": "Yusen Logistics", "yuxinwuliu": "宇鑫物流", "yw56": "燕文物流", "ywexpress": "远为快递", "yyexp": "西安运逸快递", "yyqc56": "一运全成物流", "yzswuliu": "亚洲顺物流", "zampost": "赞比亚", "zbhy56": "浩运物流", "zdepost": "直德邮", "zengyisudi": "增益速递", "zenzen": "三三国际物流", "zesexpress": "俄顺物流", "zf365": "珠峰速运", "zhaijibian": "宅急便", "zhaijisong": "宅急送", "zhaojin": "招金精炼", "zhdwl": "众辉达物流", "zhengyikuaidi": "鑫正一快递", "zhiguil": "智谷特货", "zhimakaimen": "芝麻开门", "zhitengwuliu": "志腾物流", "zhonganhuoyun": "中安物流", "zhongchuan": "众川国际", "zhonghongwl": "中宏物流", "zhonghuan": "中环快递", "zhonghuanus": "中环转运", "zhongji": "中汲物流", "zhongjiwuliu": "中技物流", "zhongsukuaidi": "中速快递", "zhongtianwanyun": "中天万运", "zhongtiewuliu": "中铁飞豹", "zhongtong": "中通快递", "zhongtongguoji": "中通国际", "zhongtongkuaiyun": "中通快运", "zhongwaiyun": "中外运速递", "zhongxinda": "忠信达", "zhongyouex": "众邮快递", "zhongyouwuliu": "中邮物流", "zhpex": "众派速递", "zhuanyunsifang": "转运四方", "zhuoshikuaiyun": "卓实快运", "zjcy56": "创运物流", "zjgj56": "振捷国际货运", "zjstky": "苏通快运", "zlink": "三真驿道", "zlxdjwl": "中粮鲜到家物流", "zrtl": "中融泰隆", "zsda56": "转瞬达集运", "zsky123": "准实快运", "zsmhwl": "明辉物流", "ztcce": "中途速递", "zteexpress": "ZTE中兴物流", "ztjieda": "泰捷达国际物流", "ztky": "中铁快运", "ztocc": "中通冷链", "ztong": "智通物流", "zy100": "中远快运", "zyzoom": "增速跨境 "}

func CompanyCodes(code string) string {
	return companyCodes[code]
}
