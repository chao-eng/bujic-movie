package controller

import (
	"github.com/bujic-movie/bujic-movie/internal/storage"
	"github.com/bujic-movie/bujic-movie/pkg/response"
	"github.com/gin-gonic/gin"
)

type FileController struct {
	storage storage.Storage
}

func NewFileController(stg storage.Storage) *FileController {
	return &FileController{storage: stg}
}

// List returns contents of a directory on the server
func (ctrl *FileController) List(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		path = "/"
	}

	items, err := ctrl.storage.List(path)
	if err != nil {
		response.InternalServerError(c, err.Error())
		return
	}

	response.Success(c, items)
}
