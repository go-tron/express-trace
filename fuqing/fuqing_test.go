package fuqing

import (
	expressTrace "github.com/go-tron/express-trace"
	"github.com/go-tron/logger"
	"testing"
)

var fuqing = &Fuqing{
	AppKey:       "204028321",
	AppSecret:    "XRTq3KBzOZsnogXIUKxyJ6qgGwNLsQAY",
	AppCode:      "e0d6240322de4170aed43c3f80818f28",
	SubscribeUrl: "http://express.eioos.com/fuqing",
	Logger:       logger.NewZap("fuqing", "info"),
}

func TestFuqing_Query(t *testing.T) {
	res, err := fuqing.Query(&QueryReq{
		No: "JD0076810060555",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Log("res", res)
}

func TestFuqing_Subscribe(t *testing.T) {
	err := fuqing.Subscribe(&expressTrace.SubscribeReq{
		OrderId: 123456,
		Number:  "JD0076810087472",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Log("succeed")
}

func TestFuqing_Company(t *testing.T) {
	result, err := fuqing.Company()
	if err != nil {
		t.Fatal(err)
	}
	t.Log("succeed", result)
}

//curl --location --request POST 'http://192.168.100.100:7031/fuqing?orderId=33334' --header 'Content-Type: application/x-www-form-urlencoded' --data-urlencode 'data={"code":"OK","no":"JD0076810087472","type":"JD","list":[{"content":"您的快件已由快递驿站代收，感谢您使用京东物流，期待再次为您服务","time":"2022-06-30 10:34:52"},{"content":"您的快件正在派送中，请您准备签收（快递员：薛兵，联系电话：18740476340）。给您服务的快递员已完成新冠疫苗接种，祝您身体健康。疫情期间，为保证安全，京东快递每日对网点消毒，快递员佩戴口罩，请您安心！","time":"2022-06-30 08:06:02"},{"content":"您的快件已到达【西安兴善营业部】","time":"2022-06-30 07:18:05"},{"content":"您的快件在【西安兴善营业部】收货完成","time":"2022-06-30 07:18:04"},{"content":"您的快件已发车","time":"2022-06-29 22:30:20"},{"content":"您的快件由【西安灞桥分拣中心】准备发往【西安兴善营业部】","time":"2022-06-29 18:01:46"},{"content":"您的快件在【西安灞桥分拣中心】分拣完成","time":"2022-06-29 15:38:48"},{"content":"您的快件已到达【西安灞桥分拣中心】","time":"2022-06-29 15:38:09"}],"state":"3","name":"京东物流","site":"www.jdwl.com","phone":"400-603-3600","logo":"https:\/\/img3.fegine.com\/express\/jd.jpg","courier":"","courierPhone":"","updateTime":"2022-06-30 10:34:52","takeTime":"0天18小时56分"}'
