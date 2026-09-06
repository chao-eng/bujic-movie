package controller

import (
	"strconv"
	"time"

	"github.com/bujic-movie/bujic-movie/internal/repository"
	"github.com/bujic-movie/bujic-movie/internal/service"
	"github.com/bujic-movie/bujic-movie/pkg/response"
	"github.com/gin-gonic/gin"
)

type MCPAPIKeyController struct {
	keySvc service.MCPAPIKeyService
}

func NewMCPAPIKeyController(keySvc service.MCPAPIKeyService) *MCPAPIKeyController {
	return &MCPAPIKeyController{keySvc: keySvc}
}

type createAPIKeyRequest struct {
	Name string `json:"name"`
}

// Create returns the plaintext secret exactly once (BR-23).
func (ctrl *MCPAPIKeyController) Create(c *gin.Context) {
	var req createAPIKeyRequest
	_ = c.ShouldBindJSON(&req)
	key, plain, err := ctrl.keySvc.Create(req.Name)
	if err != nil {
		response.InternalServerError(c, err.Error())
		return
	}
	response.Success(c, gin.H{
		"id":         key.ID,
		"name":       key.Name,
		"key":        plain, // 仅此一次
		"key_prefix": key.KeyPrefix,
		"status":     key.Status,
		"created_at": key.CreatedAt,
	})
}

func (ctrl *MCPAPIKeyController) List(c *gin.Context) {
	keys, err := ctrl.keySvc.List()
	if err != nil {
		response.InternalServerError(c, err.Error())
		return
	}
	response.Success(c, keys)
}

func (ctrl *MCPAPIKeyController) GetByID(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.BadRequest(c, "Invalid ID")
		return
	}
	key, err := ctrl.keySvc.GetByID(uint(id))
	if err != nil {
		response.NotFound(c, "API key not found")
		return
	}
	response.Success(c, key)
}

// SetStatus handles PUT /:id/enable and PUT /:id/disable.
func (ctrl *MCPAPIKeyController) SetStatus(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.BadRequest(c, "Invalid ID")
		return
	}
	status := "active"
	if c.Param("action") == "disable" {
		status = "disabled"
	}
	key, err := ctrl.keySvc.SetStatus(uint(id), status)
	if err != nil {
		response.InternalServerError(c, err.Error())
		return
	}
	response.Success(c, gin.H{
		"id":         key.ID,
		"name":       key.Name,
		"status":     key.Status,
		"updated_at": key.UpdatedAt,
	})
}

// Delete removes an API key (soft delete, records kept). DELETE /:id
func (ctrl *MCPAPIKeyController) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.BadRequest(c, "Invalid ID")
		return
	}
	if err := ctrl.keySvc.Delete(uint(id)); err != nil {
		response.NotFound(c, err.Error())
		return
	}
	response.Success(c, gin.H{"id": id})
}

// ClearRecords deletes all call records. DELETE /call-records
func (ctrl *MCPAPIKeyController) ClearRecords(c *gin.Context) {
	if err := ctrl.keySvc.ClearRecords(); err != nil {
		response.InternalServerError(c, err.Error())
		return
	}
	response.Success(c, gin.H{"message": "调用记录已清空"})
}

type recordsQuery struct {
	Tool   string `form:"tool"`
	Status string `form:"status"`
	From   string `form:"from"`
	To     string `form:"to"`
	Page   int    `form:"page"`
	Limit  int    `form:"limit"`
}

// Records returns audit records for one key (GET /:id/records).
func (ctrl *MCPAPIKeyController) Records(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.BadRequest(c, "Invalid ID")
		return
	}
	var q recordsQuery
	_ = c.ShouldBindQuery(&q)
	page, limit := normPageLimit(q.Page, q.Limit)
	recs, total, err := ctrl.queryRecords(&q, uint(id), page, limit)
	if err != nil {
		response.InternalServerError(c, err.Error())
		return
	}
	response.Success(c, gin.H{"records": recs, "total": total, "page": page, "limit": limit})
}

// AllRecords returns audit records across keys (GET /call-records).
func (ctrl *MCPAPIKeyController) AllRecords(c *gin.Context) {
	var q recordsQuery
	_ = c.ShouldBindQuery(&q)
	page, limit := normPageLimit(q.Page, q.Limit)
	recs, total, err := ctrl.queryRecords(&q, 0, page, limit)
	if err != nil {
		response.InternalServerError(c, err.Error())
		return
	}
	response.Success(c, gin.H{"records": recs, "total": total, "page": page, "limit": limit})
}

func (ctrl *MCPAPIKeyController) queryRecords(q *recordsQuery, keyID uint, page, limit int) ([]interface{}, int64, error) {
	filter := repository.MCPCallRecordFilter{Tool: q.Tool, Status: q.Status}
	if q.From != "" {
		if t, err := parseTime(q.From); err == nil {
			filter.From = &t
		}
	}
	if q.To != "" {
		if t, err := parseTime(q.To); err == nil {
			filter.To = &t
		}
	}
	if keyID != 0 {
		id := keyID
		filter.APIKeyID = &id
	}
	records, total, err := ctrl.keySvc.QueryRecords(filter, page, limit)
	if err != nil {
		return nil, 0, err
	}
	items := make([]interface{}, len(records))
	for i := range records {
		items[i] = records[i]
	}
	return items, total, nil
}

func normPageLimit(page, limit int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 200 {
		limit = 20
	}
	return page, limit
}

func parseTime(s string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, nil
}
