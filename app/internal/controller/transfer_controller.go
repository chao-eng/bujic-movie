package controller

import (
	"context"
	"strconv"

	"github.com/bujic-movie/bujic-movie/internal/service"
	"github.com/bujic-movie/bujic-movie/pkg/response"
	"github.com/gin-gonic/gin"
)

type TransferController struct {
	transferService service.TransferService
}

type TransferRequest struct {
	Path          string `json:"path" binding:"required"`
	Mode          string `json:"mode"`           // copy, move, link, softlink
	OverwriteMode string `json:"overwrite_mode"` // always, never, size, latest
}

func NewTransferController(transferService service.TransferService) *TransferController {
	return &TransferController{transferService: transferService}
}

// Transfer submits a path to the background transfer queue
func (ctrl *TransferController) Transfer(c *gin.Context) {
	var req TransferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Path is required")
		return
	}

	opts := service.TransferOptions{
		Mode:          req.Mode,
		OverwriteMode: req.OverwriteMode,
	}

	err := ctrl.transferService.SubmitTask(context.Background(), req.Path, opts)
	if err != nil {
		response.InternalServerError(c, err.Error())
		return
	}

	response.Success(c, gin.H{
		"message": "Transfer task submitted successfully",
	})
}

// GetQueue returns active transfer tasks in the queue
func (ctrl *TransferController) GetQueue(c *gin.Context) {
	queue := ctrl.transferService.GetQueue()
	response.Success(c, queue)
}

// GetHistory returns paginated transfer history logs
func (ctrl *TransferController) GetHistory(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 10
	}
	offset := (page - 1) * limit

	history, err := ctrl.transferService.GetHistory(offset, limit)
	if err != nil {
		response.InternalServerError(c, err.Error())
		return
	}

	response.Success(c, history)
}
