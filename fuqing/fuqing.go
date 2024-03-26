package fuqing

import (
	"encoding/json"
	"github.com/go-playground/validator/v10"
	"github.com/go-resty/resty/v2"
	baseError "github.com/go-tron/base-error"
	"github.com/go-tron/config"
	expressTrace "github.com/go-tron/express-trace"
	localTime "github.com/go-tron/local-time"
	"github.com/go-tron/logger"
	"strconv"
)

var (
	ErrorParam          = baseError.SystemFactory("3011", "快递查询服务参数错误:{}")
	ErrorRequest        = baseError.SystemFactory("3012", "快递查询服务连接失败:{}")
	ErrorResponse       = baseError.SystemFactory("3013", "快递查询服务返回失败:{}")
	ErrorFail           = baseError.SystemFactory("3014")
	ErrorCallbackParams = baseError.Factory("3015", "缺少参数")
)

const (
	StateWrong     = "-1"
	StateNoneYet   = "0"
	StateAccepted  = "1"
	StateInTransit = "2"
	StateDelivered = "3"
	StateQuestion  = "4"
	StateException = "5"
	StateReturned  = "6"
)

var stateCode = map[string]string{
	StateWrong:     "none",
	StateNoneYet:   "noneYet",
	StateAccepted:  "accepted",
	StateInTransit: "inTransit",
	StateDelivered: "delivered",
	StateQuestion:  "question",
	StateException: "exception",
	StateReturned:  "returned",
}

func StateCode(code string) string {
	return stateCode[code]
}

var validate *validator.Validate

func init() {
	validate = validator.New()
}

type Fuqing struct {
	AppKey       string
	AppSecret    string
	AppCode      string
	SubscribeUrl string
	Logger       logger.Logger
}

func NewWithConfig(c *config.Config) *Fuqing {
	return New(&Fuqing{
		AppKey:       c.GetString("fuqing.appKey"),
		AppSecret:    c.GetString("fuqing.appSecret"),
		AppCode:      c.GetString("fuqing.appCode"),
		SubscribeUrl: c.GetString("fuqing.subscribeUrl"),
		Logger:       logger.NewZapWithConfig(c, "fuqing", "error"),
	})
}

func New(c *Fuqing) *Fuqing {
	if c == nil {
		panic("config 必须设置")
	}
	if c.AppKey == "" {
		panic("AppKey 必须设置")
	}
	if c.AppSecret == "" {
		panic("AppSecret 必须设置")
	}
	if c.AppCode == "" {
		panic("AppCode 必须设置")
	}
	if c.SubscribeUrl == "" {
		panic("SubscribeUrl 必须设置")
	}
	if c.Logger == nil {
		panic("Logger 必须设置")
	}
	return c
}

type QueryReq struct {
	No   string `json:"no" validate:"required"`
	Type string `json:"type"`
}
type QueryResponse struct {
	Status string   `json:"status"` //status 0:正常查询 201:快递单号错误 203:快递公司不存在 204:快递公司识别失败 205:没有信息 207:该单号被限制，错误单号
	Msg    string   `json:"msg"`
	Result QueryRes `json:"result"`
}
type QueryRes struct {
	Number         string          `json:"number"`         //快递单号
	Type           string          `json:"type"`           //快递缩写
	Deliverystatus string          `json:"deliverystatus"` //0：快递收件(揽件)1.在途中 2.正在派件 3.已签收 4.派送失败 5.疑难件 6.退件签收
	Issign         string          `json:"issign"`         //是否签收
	ExpName        string          `json:"expName"`        //快递公司名称
	ExpSite        string          `json:"expSite"`        //快递公司官网
	ExpPhone       string          `json:"expPhone"`       //快递公司电话
	Logo           string          `json:"logo"`           //快递公司LOGO
	Courier        string          `json:"courier"`        //快递员
	CourierPhone   string          `json:"courierPhone"`   //快递员电话
	UpdateTime     *localTime.Time `json:"updateTime"`     //快递轨迹信息最新时间
	TakeTime       string          `json:"takeTime"`       //发货到收货消耗时长
	List           []struct {
		Time   *localTime.Time `json:"time"`
		Status string          `json:"status"`
	} `json:"list"` //结果集
}

func (c *Fuqing) Query(req *QueryReq) (res *QueryRes, err error) {

	var resBody = ""
	defer func() {
		c.Logger.Info("",
			c.Logger.Field("number", req.No),
			c.Logger.Field("error", err),
			c.Logger.Field("response", resBody),
		)
	}()

	if err := validate.Struct(req); err != nil {
		return nil, ErrorParam(err)
	}

	var data = make(map[string]string)
	data["no"] = req.No
	if req.Type != "" {
		data["type"] = req.Type
	}

	c.Logger.Info("开始请求",
		c.Logger.Field("number", req.No),
	)
	url := "http://wuliu.market.alicloudapi.com/kdi"

	request := resty.New().R()
	request = request.SetHeaders(map[string]string{
		"Authorization": "APPCODE " + c.AppCode,
	})
	request = request.SetQueryParams(data)
	response, err := request.Get(url)
	if err != nil {
		return nil, ErrorRequest(err)
	}
	resBody = response.String()
	var resp QueryResponse
	if err := json.Unmarshal(response.Body(), &resp); err != nil {
		return nil, ErrorResponse(err)
	}

	if resp.Status != "0" {
		var errorMsg = "请求失败"
		if resp.Msg != "" {
			errorMsg = resp.Msg
		}
		return nil, ErrorFail(errorMsg)
	}

	return &resp.Result, nil
}

type SubscribeResponse struct {
	Orderid string `json:"orderid"`
	Status  bool   `json:"status"`
	Code    string `json:"code"`
	No      string `json:"no"`
	Type    string `json:"type"`
	Url     string `json:"url"`
	Message string `json:"message"`
}

type SubscribeCallback struct {
	Code         string          `json:"code"`         //-1单号或快递公司错误；201快递单号错误；203 快递公司不存在；204 错误单号重复；205 没有轨迹；207 该单号被限制，错误单号；OK 查询成功
	No           string          `json:"no"`           //快递单号
	Type         string          `json:"type"`         //快递缩写
	State        string          `json:"state"`        //物流状态：-1：单号或代码错误；0：暂无轨迹；1:快递收件；2：在途中；3：签收；4：问题件 5.疑难件 6.退件签收
	Name         string          `json:"name"`         //快递名称
	Site         string          `json:"site"`         //快递公司官网
	Phone        string          `json:"phone"`        //快递公司电话
	Logo         string          `json:"logo"`         //快递公司logo
	Courier      string          `json:"courier"`      //快递员
	CourierPhone string          `json:"courierPhone"` //快递员电话
	UpdateTime   *localTime.Time `json:"updateTime"`   //快递轨迹信息最新时间
	TakeTime     string          `json:"takeTime"`     //发货到收货消耗时长
	List         []struct {
		Time    *localTime.Time `json:"time"`
		Content string          `json:"content"`
	} `json:"list"` //结果集
}

func (c *Fuqing) Subscribe(req *expressTrace.SubscribeReq) (err error) {

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

	var data = make(map[string]string)
	data["no"] = req.Number
	data["url"] = c.SubscribeUrl + "?orderId=" + strconv.FormatInt(req.OrderId, 10)
	if req.Company != "" {
		data["type"] = req.Company
	}

	c.Logger.Info("开始请求",
		c.Logger.Field("number", req.Number),
	)
	url := "http://expfeeds.market.alicloudapi.com/expresspush"

	request := resty.New().R()
	request = request.SetHeaders(map[string]string{
		"Authorization": "APPCODE " + c.AppCode,
	})
	request = request.SetQueryParams(data)
	response, err := request.Get(url)
	if err != nil {
		return ErrorRequest(err)
	}
	resBody = response.String()
	var resp SubscribeResponse
	if err := json.Unmarshal(response.Body(), &resp); err != nil {
		return ErrorResponse(err)
	}

	if !resp.Status {
		var errorMsg = "请求失败"
		if resp.Message != "" {
			errorMsg = resp.Message
		}
		return ErrorFail(errorMsg)
	}

	return nil
}

func (c *Fuqing) SubscribeCallback(orderId int64, data map[string]string) (res *expressTrace.SubscribeRes, err error) {
	if orderId == 0 {
		return nil, ErrorCallbackParams("orderId")
	}
	if data["data"] == "" {
		return nil, ErrorCallbackParams("data")
	}
	callback := &SubscribeCallback{}
	if err := json.Unmarshal([]byte(data["data"]), callback); err != nil {
		return nil, err
	}

	signed := 0
	if callback.State == StateDelivered {
		signed = 1
	}

	var lastTraceInfo = ""
	var lastTraceTime *localTime.Time
	if len(callback.List) > 0 {
		lastTraceInfo = callback.List[0].Content
		lastTraceTime = callback.List[0].Time
	}

	var traces = make([]expressTrace.Trace, 0)
	for _, v := range callback.List {
		traces = append(traces, expressTrace.Trace{
			Time: v.Time,
			Info: v.Content,
		})
	}

	return &expressTrace.SubscribeRes{
		OrderId:       orderId,
		Number:        callback.No,
		Signed:        signed,
		Status:        StateCode(callback.State),
		LastTraceInfo: lastTraceInfo,
		LastTraceTime: lastTraceTime,
		Traces:        traces,
		CompanyName:   callback.Name,
		CompanyCode:   callback.Type,
		CompanySite:   callback.Site,
		CompanyPhone:  callback.Phone,
		CompanyLogo:   callback.Logo,
	}, nil
}

func (c *Fuqing) Company() (res map[string]interface{}, err error) {
	url := "http://expfeeds.market.alicloudapi.com/pushExpressLists"
	request := resty.New().R()
	request = request.SetHeaders(map[string]string{
		"Authorization": "APPCODE " + c.AppCode,
	})
	response, err := request.Get(url)
	if err != nil {
		return nil, ErrorRequest(err)
	}
	res = make(map[string]interface{})
	if err := json.Unmarshal(response.Body(), &res); err != nil {
		return nil, ErrorResponse(err)
	}
	return res, nil
}
