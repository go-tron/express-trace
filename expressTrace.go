package expressTrace

import "github.com/go-tron/local-time"

type SubscribeReq struct {
	OrderId int64  `json:"orderId,string" validate:"required"`
	Number  string `json:"number" validate:"required"`
	Company string `json:"company"`
}

type SubscribeRes struct {
	OrderId       int64           `json:"orderId,string" validate:"required"`
	Number        string          `json:"number" validate:"required"`
	Signed        int             `json:"signed"`
	Status        string          `json:"status"`
	LastTraceInfo string          `json:"lastTraceInfo"`
	LastTraceTime *localTime.Time `json:"lastTraceTime"`
	Traces        []Trace         `json:"traces"`
	CompanyName   string          `json:"companyName"`
	CompanyCode   string          `json:"companyCode"`
	CompanySite   string          `json:"companySite"`
	CompanyPhone  string          `json:"companyPhone"`
	CompanyLogo   string          `json:"companyLogo"`
}

type Trace struct {
	Time *localTime.Time `json:"time"`
	Info string          `json:"info"`
}

type ExpressTrace interface {
	Subscribe(*SubscribeReq) error
	SubscribeCallback(int64, map[string]string) (*SubscribeRes, error)
}
