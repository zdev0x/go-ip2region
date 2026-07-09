package model

// Region 为结构化后的 IP 归属地信息。
// ip2region 原始串格式：Country|Province|City|ISP|iso-alpha2-code
type Region struct {
	IP       string `json:"ip"`
	Country  string `json:"country"`
	Province string `json:"province"`
	City     string `json:"city"`
	ISP      string `json:"isp"`
	Code     string `json:"code"`
	Raw      string `json:"region"`
}

// BatchItem 批量查询的单条结果，保持与入参顺序一致。
// 单条失败时通过 Error 字段体现，不影响整批其他项。
type BatchItem struct {
	Region
	Error string `json:"error,omitempty"`
}

// BatchRequest 批量查询请求体。
type BatchRequest struct {
	IPs []string `json:"ips"`
}

// BatchResponse 批量查询响应。
type BatchResponse struct {
	Count   int         `json:"count"`
	Results []BatchItem `json:"results"`
}

// ErrorResponse 统一错误响应结构。
type ErrorResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
