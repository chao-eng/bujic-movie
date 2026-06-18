package controller

import (
	"context"
	"strconv"
	"time"

	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"github.com/bujic-movie/bujic-movie/internal/service"
	"github.com/bujic-movie/bujic-movie/pkg/response"
	"github.com/gin-gonic/gin"
)

type MediaLibraryController struct {
	svc service.MediaLibraryService
}

func NewMediaLibraryController(svc service.MediaLibraryService) *MediaLibraryController {
	return &MediaLibraryController{svc: svc}
}

func (ctrl *MediaLibraryController) List(c *gin.Context) {
	libs, err := ctrl.svc.List()
	if err != nil {
		response.InternalServerError(c, err.Error())
		return
	}
	response.Success(c, libs)
}

func (ctrl *MediaLibraryController) GetByID(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.BadRequest(c, "Invalid ID")
		return
	}
	lib, err := ctrl.svc.GetByID(uint(id))
	if err != nil {
		response.NotFound(c, "Media library not found")
		return
	}
	response.Success(c, lib)
}

func (ctrl *MediaLibraryController) Create(c *gin.Context) {
	var lib entity.MediaLibrary
	if err := c.ShouldBindJSON(&lib); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if lib.Name == "" || lib.URL == "" {
		response.BadRequest(c, "Name and url are required")
		return
	}
	if !isValidLibraryType(lib.Type) {
		response.BadRequest(c, "type must be 'emby', 'jellyfin' or 'plex'")
		return
	}
	if err := ctrl.svc.Create(&lib); err != nil {
		response.InternalServerError(c, err.Error())
		return
	}
	response.Success(c, lib)
}

func (ctrl *MediaLibraryController) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.BadRequest(c, "Invalid ID")
		return
	}
	var lib entity.MediaLibrary
	if err := c.ShouldBindJSON(&lib); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if lib.Type != "" && !isValidLibraryType(lib.Type) {
		response.BadRequest(c, "type must be 'emby', 'jellyfin' or 'plex'")
		return
	}
	lib.ID = uint(id)
	if err := ctrl.svc.Update(&lib); err != nil {
		response.InternalServerError(c, err.Error())
		return
	}
	response.Success(c, lib)
}

func (ctrl *MediaLibraryController) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.BadRequest(c, "Invalid ID")
		return
	}
	if err := ctrl.svc.Delete(uint(id)); err != nil {
		response.InternalServerError(c, err.Error())
		return
	}
	response.Success(c, gin.H{"message": "Media library deleted successfully"})
}

// Test verifies connectivity to the configured media server.
func (ctrl *MediaLibraryController) Test(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.BadRequest(c, "Invalid ID")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	if err := ctrl.svc.TestConnection(ctx, uint(id)); err != nil {
		response.InternalServerError(c, err.Error())
		return
	}
	response.Success(c, gin.H{"message": "连接成功"})
}

// Refresh triggers a library refresh on the media server.
func (ctrl *MediaLibraryController) Refresh(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.BadRequest(c, "Invalid ID")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	if err := ctrl.svc.Refresh(ctx, uint(id)); err != nil {
		response.InternalServerError(c, err.Error())
		return
	}
	response.Success(c, gin.H{"message": "刷新通知已发送"})
}

type probeRequest struct {
	Type   string `json:"type"`
	URL    string `json:"url"`
	APIKey string `json:"api_key"`
}

// Probe lists the libraries on a media server identified by raw credentials,
// so the edit form can populate the library dropdown before saving.
func (ctrl *MediaLibraryController) Probe(c *gin.Context) {
	var req probeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if req.URL == "" || !isValidLibraryType(req.Type) {
		response.BadRequest(c, "type 与 url 必填")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	libs, err := ctrl.svc.ProbeLibraries(ctx, req.Type, req.URL, req.APIKey)
	if err != nil {
		response.InternalServerError(c, err.Error())
		return
	}
	response.Success(c, libs)
}

// Status returns the online state of every configured media server (heartbeat).
func (ctrl *MediaLibraryController) Status(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	response.Success(c, ctrl.svc.Statuses(ctx))
}

func isValidLibraryType(t string) bool {
	return t == "emby" || t == "jellyfin" || t == "plex"
}
