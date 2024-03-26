package kuaidi100

import (
	expressTrace "github.com/go-tron/express-trace"
	"github.com/go-tron/logger"
	"testing"
)

var kuaidi100 = New(&Kuaidi100{
	key:          "BoQtnsPM7007",
	Customer:     "994F35FF7ECA32CE736F02BE3C0545CE",
	SubscribeUrl: "http://express.eioos.com/kuaidi100",
	SignSalt:     "123",
	Logger:       logger.NewZap("kuaidi100", "info"),
})

func TestKuaidi100_Subscribe(t *testing.T) {
	err := kuaidi100.Subscribe(&expressTrace.SubscribeReq{
		OrderId: 123456,
		Number:  "JD0076810060555",
		Company: "JD",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Log("succeed")
}

//curl --location --request POST 'http://192.168.100.100:7031/kuaidi100?orderId=33333' --header 'Content-Type: application/x-www-form-urlencoded' --data-urlencode 'param={"status":"shutdown","billstatus":"check","message":"","lastResult":{"message":"ok","nu":"JD0076810060555","ischeck":"1","com":"jd","status":"200","data":[{"time":"2022-06-30 10:34:33","context":"您的快件已由快递驿站代收，感谢您使用京东物流，期待再次为您服务","ftime":"2022-06-30 10:34:33","areaCode":null,"areaName":null,"status":"投柜或站签收","location":"","areaCenter":null,"areaPinYin":null,"statusCode":"304"},{"time":"2022-06-30 08:27:50","context":"您的快件正在派送中，请您准备签收（快递员：薛兵，联系电话：18740476340）。给您服务的快递员已完成新冠疫苗接种，祝您身体健康。疫情期间，为保证安全，京东快递每日对网点消毒，快递员佩戴口罩，请您安心！","ftime":"2022-06-30 08:27:50","areaCode":null,"areaName":null,"status":"在途","location":"","areaCenter":null,"areaPinYin":null,"statusCode":"0"},{"time":"2022-06-29 22:30:20","context":"您的快件已发车","ftime":"2022-06-29 22:30:20","areaCode":null,"areaName":null,"status":"在途","location":"","areaCenter":null,"areaPinYin":null,"statusCode":"0"},{"time":"2022-06-29 22:28:50","context":"您的快件由【西安灞桥分拣中心】准备发往【西安兴善营业部】","ftime":"2022-06-29 22:28:50","areaCode":"CN610111000000","areaName":"陕西,西安市,灞桥区","status":"干线","location":"","areaCenter":"109.064671,34.273409","areaPinYin":"ba qiao qu","statusCode":"1002"},{"time":"2022-06-29 22:28:45","context":"您的快件在【西安灞桥分拣中心】分拣完成","ftime":"2022-06-29 22:28:45","areaCode":"CN610111000000","areaName":"陕西,西安市,灞桥区","status":"干线","location":"","areaCenter":"109.064671,34.273409","areaPinYin":"ba qiao qu","statusCode":"1002"}],"state":"304","condition":"00","routeInfo":{"from":{"number":"CN610111000000","name":"陕西,西安市,灞桥区"},"cur":{"number":"CN610111000000","name":"陕西,西安市,灞桥区"},"to":{"number":"CN610111000000","name":"陕西,西安市,灞桥区"}},"isLoop":false}}' --data-urlencode 'sign=315EDA9CDABADA878C643EBFE3DBCF1B'
