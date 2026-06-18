package controller

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"github.com/bujic-movie/bujic-movie/internal/service"
	"github.com/bujic-movie/bujic-movie/pkg/response"
	"github.com/gin-gonic/gin"
)

type NotifyChannelController struct {
	svc service.MessageNotifyService
}

func NewNotifyChannelController(svc service.MessageNotifyService) *NotifyChannelController {
	return &NotifyChannelController{svc: svc}
}

// notifyChannelRequest accepts config as a JSON object for convenience.
type notifyChannelRequest struct {
	Name    string            `json:"name"`
	Type    string            `json:"type"`
	Enabled *bool             `json:"enabled"`
	Config  map[string]string `json:"config"`
}

type notifyChannelResp struct {
	ID      uint              `json:"id"`
	Name    string            `json:"name"`
	Type    string            `json:"type"`
	Enabled bool              `json:"enabled"`
	Config  map[string]string `json:"config"`
}

func toNotifyResp(ch *entity.NotifyChannel) notifyChannelResp {
	cfg := map[string]string{}
	if ch.Config != "" {
		_ = json.Unmarshal([]byte(ch.Config), &cfg)
	}
	return notifyChannelResp{ID: ch.ID, Name: ch.Name, Type: ch.Type, Enabled: ch.Enabled, Config: cfg}
}

func (ctrl *NotifyChannelController) Types(c *gin.Context) {
	response.Success(c, ctrl.svc.ChannelTypes())
}

func (ctrl *NotifyChannelController) List(c *gin.Context) {
	channels, err := ctrl.svc.List()
	if err != nil {
		response.InternalServerError(c, err.Error())
		return
	}
	out := make([]notifyChannelResp, 0, len(channels))
	for i := range channels {
		out = append(out, toNotifyResp(&channels[i]))
	}
	response.Success(c, out)
}

func (ctrl *NotifyChannelController) GetByID(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.BadRequest(c, "Invalid ID")
		return
	}
	ch, err := ctrl.svc.GetByID(uint(id))
	if err != nil {
		response.NotFound(c, "Channel not found")
		return
	}
	response.Success(c, toNotifyResp(ch))
}

func (ctrl *NotifyChannelController) Create(c *gin.Context) {
	var req notifyChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if req.Name == "" || req.Type == "" {
		response.BadRequest(c, "name 与 type 必填")
		return
	}
	ch := &entity.NotifyChannel{
		Name:    req.Name,
		Type:    req.Type,
		Enabled: req.Enabled == nil || *req.Enabled,
		Config:  marshalConfig(req.Config),
	}
	if err := ctrl.svc.Create(ch); err != nil {
		response.InternalServerError(c, err.Error())
		return
	}
	response.Success(c, toNotifyResp(ch))
}

func (ctrl *NotifyChannelController) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.BadRequest(c, "Invalid ID")
		return
	}
	var req notifyChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	ch := &entity.NotifyChannel{
		ID:      uint(id),
		Name:    req.Name,
		Type:    req.Type,
		Enabled: req.Enabled == nil || *req.Enabled,
		Config:  marshalConfig(req.Config),
	}
	if err := ctrl.svc.Update(ch); err != nil {
		response.InternalServerError(c, err.Error())
		return
	}
	response.Success(c, toNotifyResp(ch))
}

func (ctrl *NotifyChannelController) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.BadRequest(c, "Invalid ID")
		return
	}
	if err := ctrl.svc.Delete(uint(id)); err != nil {
		response.InternalServerError(c, err.Error())
		return
	}
	response.Success(c, gin.H{"message": "Channel deleted successfully"})
}

func (ctrl *NotifyChannelController) Test(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.BadRequest(c, "Invalid ID")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
	defer cancel()
	if err := ctrl.svc.Test(ctx, uint(id)); err != nil {
		response.InternalServerError(c, err.Error())
		return
	}
	response.Success(c, gin.H{"message": "测试消息已发送"})
}

func marshalConfig(cfg map[string]string) string {
	if len(cfg) == 0 {
		return "{}"
	}
	b, _ := json.Marshal(cfg)
	return string(b)
}
