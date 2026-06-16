package controller

import (
	"github.com/bujic-movie/bujic-movie/pkg/response"
	"github.com/gin-gonic/gin"
)

type HealthController struct{}

func NewHealthController() *HealthController {
	return &HealthController{}
}

// Check returns a basic server health status
func (ctrl *HealthController) Check(c *gin.Context) {
	response.Success(c, gin.H{"status": "ok"})
}
