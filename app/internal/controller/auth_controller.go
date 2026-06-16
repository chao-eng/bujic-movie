package controller

import (
	"github.com/bujic-movie/bujic-movie/internal/config"
	"github.com/bujic-movie/bujic-movie/internal/middleware"
	"github.com/bujic-movie/bujic-movie/pkg/response"
	"github.com/gin-gonic/gin"
)

type AuthController struct{}

type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func NewAuthController() *AuthController {
	return &AuthController{}
}

// Login checks credentials and returns a JWT token
func (ctrl *AuthController) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Username and password are required")
		return
	}

	cfg := config.GlobalConfig
	if req.Username != cfg.Server.Username || req.Password != cfg.Server.Password {
		response.Unauthorized(c, "Invalid username or password")
		return
	}

	token, err := middleware.GenerateToken(req.Username)
	if err != nil {
		response.InternalServerError(c, "Failed to generate authentication token")
		return
	}

	response.Success(c, gin.H{
		"token": token,
	})
}
