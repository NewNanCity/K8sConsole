package model

// Response 通用API响应结构
type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// NewResponse 创建新的响应
func NewResponse(code int, message string, data interface{}) Response {
	return Response{
		Code:    code,
		Message: message,
		Data:    data,
	}
}

// SuccessResponse 创建成功响应
func SuccessResponse(data interface{}) Response {
	return Response{
		Code:    200,
		Message: "操作成功",
		Data:    data,
	}
}

// ErrorResponse 创建错误响应
func ErrorResponse(code int, message string) Response {
	if code == 0 {
		code = 500
	}
	return Response{
		Code:    code,
		Message: message,
	}
}

// PagedResponse 分页响应结构
type PagedResponse struct {
	Response
	Total      int64       `json:"total"`
	PageSize   int         `json:"page_size"`
	PageNumber int         `json:"page_number"`
	Pages      int         `json:"pages"`
	Items      interface{} `json:"items"`
}

// NewPagedResponse 创建新的分页响应
func NewPagedResponse(total int64, pageSize, pageNumber int, items interface{}) PagedResponse {
	pages := 0
	if pageSize > 0 {
		pages = int((total + int64(pageSize) - 1) / int64(pageSize))
	}

	return PagedResponse{
		Response: Response{
			Code:    200,
			Message: "操作成功",
		},
		Total:      total,
		PageSize:   pageSize,
		PageNumber: pageNumber,
		Pages:      pages,
		Items:      items,
	}
}
