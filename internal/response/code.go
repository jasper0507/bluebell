package response

import "net/http"

// Code 业务错误码
type Code string

const (
	CodeOK                 Code = "OK"
	CodeInvalidParams      Code = "INVALID_PARAMS"
	CodeUsernameExists     Code = "USERNAME_EXISTS"
	CodeInvalidCredentials Code = "INVALID_CREDENTIALS"
	CodeUnauthorized       Code = "UNAUTHORIZED"
	CodeForbidden          Code = "FORBIDDEN"
	CodeCommunityNotFound  Code = "COMMUNITY_NOT_FOUND"
	CodePostNotFound       Code = "POST_NOT_FOUND"
	CodeCommentNotFound    Code = "COMMENT_NOT_FOUND"
	CodeInternalError      Code = "INTERNAL_ERROR"
)

// Message 获取错误码对应的响应消息
func (c Code) Message() string {
	switch c {
	case CodeOK:
		return "成功"
	case CodeInvalidParams:
		return "请求参数无效"
	case CodeUsernameExists:
		return "用户名已存在"
	case CodeInvalidCredentials:
		return "用户名或密码错误"
	case CodeUnauthorized:
		return "未登录或登录已过期"
	case CodeForbidden:
		return "无权执行此操作"
	case CodeCommunityNotFound:
		return "社区不存在"
	case CodePostNotFound:
		return "帖子不存在"
	case CodeCommentNotFound:
		return "评论不存在"
	case CodeInternalError:
		return "服务器内部错误"
	default:
		return "服务器内部错误"
	}
}

// HTTPStatus 获取错误码对应的 HTTP 状态码
func (c Code) HTTPStatus() int {
	switch c {
	case CodeOK:
		return http.StatusOK
	case CodeInvalidParams:
		return http.StatusBadRequest
	case CodeUsernameExists:
		return http.StatusConflict
	case CodeInvalidCredentials, CodeUnauthorized:
		return http.StatusUnauthorized
	case CodeForbidden:
		return http.StatusForbidden
	case CodeCommunityNotFound, CodePostNotFound, CodeCommentNotFound:
		return http.StatusNotFound
	case CodeInternalError:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}
