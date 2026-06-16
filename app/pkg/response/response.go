package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type Response struct {
	Code int         `json:"code"`
	Msg  string      `json:"msg"`
	Data interface{} `json:"data,omitempty"`
}

// Success returns a standard success JSON response
func Success(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Response{
		Code: 0,
		Msg:  "success",
		Data: data,
	})
}

// Error returns a standard error JSON response with custom HTTP status code
func Error(c *gin.Context, httpStatus int, code int, msg string) {
	c.JSON(httpStatus, Response{
		Code: code,
		Msg:  msg,
	})
}

// BadRequest returns a 400 Bad Request error
func BadRequest(c *gin.Context, msg string) {
	Error(c, http.StatusBadRequest, 40001, msg)
}

// Unauthorized returns a 401 Unauthorized error
func Unauthorized(c *gin.Context, msg string) {
	Error(c, http.StatusUnauthorized, 40101, msg)
}

// Forbidden returns a 403 Forbidden error
func Forbidden(c *gin.Context, msg string) {
	Error(c, http.StatusForbidden, 40301, msg)
}

// NotFound returns a 404 Not Found error
func NotFound(c *gin.Context, msg string) {
	Error(c, http.StatusNotFound, 40401, msg)
}

// InternalServerError returns a 500 Internal Server Error
func InternalServerError(c *gin.Context, msg string) {
	Error(c, http.StatusInternalServerError, 50001, msg)
}
