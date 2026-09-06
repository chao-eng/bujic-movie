package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bujic-movie/bujic-movie/internal/service"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

const (
	toolPing           = "mcp_ping"
	toolListMediaCards = "list_media_cards"
	toolQueryMediaList = "query_media_list"
	toolQueryMediaSubs = "query_media_subtitles"
	toolFetchSubtitle  = "fetch_subtitle"
	toolUploadSubtitle = "upload_subtitle"
)

// registerTools defines the tools and their handlers. Each handler:
// 1) parses+validates arguments, 2) builds a redacted input_meta, 3) acquires
// the concurrency semaphore, 4) calls the service, 5) records an audit entry.
func registerTools(
	server *mcpserver.MCPServer,
	svc service.SubtitleAgentService,
	sem chan struct{},
	rec *recordWriter,
) {
	server.AddTool(mcp.NewTool(toolPing,
		mcp.WithDescription("连通性/鉴权自测：返回服务端时间与版本，不读业务数据、不落调用记录。"),
	), pingHandler)

	server.AddTool(mcp.NewTool(toolListMediaCards,
		mcp.WithDescription("枚举全部媒体卡（MediaCard）：返回每张卡的 id/name/media_type/archive_path/download_path/is_default/watch_directory。用于确定媒体库范围（media_card_id）；agent 应据此决定后续查询/操作落到哪张卡。"),
	), callHandler(sem, rec, toolListMediaCards, func(ctx context.Context, req mcp.CallToolRequest, meta *inputMeta) (any, error) {
		res, err := svc.ListMediaCards(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]any{"cards": res}, nil
	}))

	server.AddTool(mcp.NewTool(toolQueryMediaList,
		mcp.WithDescription("查询媒体库列表（含字幕状态）。返回每项的 media_id/title/type/path/has_subtitle/subtitle_status/languages/missing_subtitles。不传 media_card_id 时范围=全部媒体卡。"),
		mcp.WithString("media_type", mcp.Description("movie 或 tv；缺省全部")),
		mcp.WithString("query", mcp.Description("标题模糊关键词，≤100 字符")),
		mcp.WithNumber("media_card_id", mcp.Description("媒体库范围：省略或 0=全部卡，>0=指定卡（先用 list_media_cards 枚举）")),
		mcp.WithNumber("page", mcp.Description("页码，从 1 起；缺省 1")),
		mcp.WithNumber("limit", mcp.Description("每页条数 1~200；缺省 50")),
	), callHandler(sem, rec, toolQueryMediaList, func(ctx context.Context, req mcp.CallToolRequest, meta *inputMeta) (any, error) {
		var p struct {
			MediaType   string `json:"media_type"`
			Query       string `json:"query"`
			MediaCardID uint   `json:"media_card_id"`
			Page        int    `json:"page"`
			Limit       int    `json:"limit"`
		}
		if err := bindArgs(req, &p); err != nil {
			return nil, err
		}
		if len(p.Query) > 100 {
			return nil, errors.New("query too long (max 100)")
		}
		meta.set("media_type", p.MediaType)
		meta.set("media_card_id", p.MediaCardID)
		meta.set("query", p.Query)
		meta.set("page", p.Page)
		meta.set("limit", p.Limit)

		res, err := svc.QueryMediaList(ctx, service.MediaListRequest{
			MediaType:   p.MediaType,
			Query:       p.Query,
			MediaCardID: p.MediaCardID,
			Page:        p.Page,
			Limit:       p.Limit,
		})
		if err != nil {
			return nil, err
		}
		return res, nil
	}))

	server.AddTool(mcp.NewTool(toolQueryMediaSubs,
		mcp.WithDescription("查询单个媒体/视频的全部字幕（外挂 + 内嵌）。media_id 或 path 二选一；传季目录 path 返回该季各集。media_card_id 省略或 0=任意卡；>0=限定该卡。"),
		mcp.WithNumber("media_id", mcp.Description("媒体记录 ID（medias.id）")),
		mcp.WithString("path", mcp.Description("视频文件或季目录绝对路径")),
		mcp.WithNumber("media_card_id", mcp.Description("媒体库范围：省略或 0=任意卡，>0=指定卡")),
		mcp.WithBoolean("include_internal", mcp.Description("是否探测内嵌轨道；缺省 true")),
	), callHandler(sem, rec, toolQueryMediaSubs, func(ctx context.Context, req mcp.CallToolRequest, meta *inputMeta) (any, error) {
		var p struct {
			MediaID         uint   `json:"media_id"`
			Path            string `json:"path"`
			MediaCardID     uint   `json:"media_card_id"`
			IncludeInternal *bool  `json:"include_internal"`
		}
		if err := bindArgs(req, &p); err != nil {
			return nil, err
		}
		includeInternal := true
		if p.IncludeInternal != nil {
			includeInternal = *p.IncludeInternal
		}
		meta.set("media_id", p.MediaID)
		meta.set("include_internal", includeInternal)

		res, err := svc.QuerySubtitles(ctx, service.QuerySubtitlesRequest{
			MediaID:         p.MediaID,
			Path:            p.Path,
			MediaCardID:     p.MediaCardID,
			IncludeInternal: p.IncludeInternal,
		})
		if err != nil {
			return nil, err
		}
		return res, nil
	}))

	server.AddTool(mcp.NewTool(toolFetchSubtitle,
		mcp.WithDescription("获取一条字幕的内容。外挂用 path；内嵌用 video_path+internal_index。图像字幕返回 content_base64 与 is_image=true。media_card_id 省略或 0=任意卡；>0=限定该卡。"),
		mcp.WithString("path", mcp.Description("外挂字幕文件绝对路径")),
		mcp.WithString("video_path", mcp.Description("视频绝对路径（内嵌轨道时用）")),
		mcp.WithNumber("internal_index", mcp.Description("内嵌轨道序号")),
		mcp.WithNumber("media_card_id", mcp.Description("媒体库范围：省略或 0=任意卡，>0=指定卡")),
	), callHandler(sem, rec, toolFetchSubtitle, func(ctx context.Context, req mcp.CallToolRequest, meta *inputMeta) (any, error) {
		var p struct {
			Path          string `json:"path"`
			VideoPath     string `json:"video_path"`
			InternalIndex int    `json:"internal_index"`
			MediaCardID   uint   `json:"media_card_id"`
		}
		if err := bindArgs(req, &p); err != nil {
			return nil, err
		}
		if p.Path == "" && p.VideoPath == "" {
			return nil, errors.New("path 或 video_path 必须提供其一")
		}
		meta.set("is_internal", p.Path == "")
		meta.set("internal_index", p.InternalIndex)
		meta.set("media_card_id", p.MediaCardID)

		res, err := svc.FetchSubtitle(ctx, service.FetchSubtitleRequest{
			Path:          p.Path,
			VideoPath:     p.VideoPath,
			InternalIndex: p.InternalIndex,
			MediaCardID:   p.MediaCardID,
		})
		if err != nil {
			return nil, err
		}
		return res, nil
	}))

	server.AddTool(mcp.NewTool(toolUploadSubtitle,
		mcp.WithDescription("上传一条字幕到媒体库。video_path 必填；content（文本）或 base64（原始字节）二选一；language 建议 zh-CN；format 可缺省按内容推断。"),
		mcp.WithString("video_path", mcp.Description("目标视频绝对路径")),
		mcp.WithString("subtitle_content", mcp.Description("字幕文本（UTF-8）")),
		mcp.WithString("subtitle_base64", mcp.Description("字幕原始字节 base64")),
		mcp.WithString("format", mcp.Description("srt/ass/ssa/vtt/sub")),
		mcp.WithString("language", mcp.Description("语言标记，如 zh-CN")),
	), callHandler(sem, rec, toolUploadSubtitle, func(ctx context.Context, req mcp.CallToolRequest, meta *inputMeta) (any, error) {
		var p struct {
			VideoPath       string `json:"video_path"`
			SubtitleContent string `json:"subtitle_content"`
			SubtitleBase64  string `json:"subtitle_base64"`
			Format          string `json:"format"`
			Language        string `json:"language"`
		}
		if err := bindArgs(req, &p); err != nil {
			return nil, err
		}
		if p.VideoPath == "" {
			return nil, errors.New("video_path 必填")
		}
		if p.SubtitleContent == "" && p.SubtitleBase64 == "" {
			return nil, errors.New("subtitle_content 或 subtitle_base64 必填")
		}
		// redaction: never log content/path
		meta.set("language", p.Language)
		meta.set("format", p.Format)
		meta.set("byte_size", len(p.SubtitleContent)+len(p.SubtitleBase64))
		meta.set("has_base64", p.SubtitleBase64 != "")

		res, err := svc.UploadSubtitle(ctx, service.UploadSubtitleRequest{
			VideoPath:       p.VideoPath,
			SubtitleContent: p.SubtitleContent,
			SubtitleBase64:  p.SubtitleBase64,
			Format:          p.Format,
			Language:        p.Language,
		})
		if err != nil {
			return nil, err
		}
		return res, nil
	}))
}

// ---- shared handler plumbing ----

func pingHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultJSON(map[string]any{
		"ok":          true,
		"server_time": time.Now().Format(time.RFC3339),
		"version":     "v0.3.0",
	})
}

// callHandler wraps a business handler with semaphore + audit recording.
func callHandler(
	sem chan struct{},
	rec *recordWriter,
	toolName string,
	fn func(ctx context.Context, req mcp.CallToolRequest, meta *inputMeta) (any, error),
) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		keyID, _ := apiKeyIDFromContext(ctx)
		start := time.Now()
		meta := newInputMeta()

		// BR-21 semaphore (skip for ping; ping isn't routed here anyway)
		select {
		case sem <- struct{}{}:
			defer func() { <-sem }()
		case <-ctx.Done():
			return errorResult("timeout waiting for concurrency slot", ctx.Err()), nil
		}

		out, err := fn(ctx, req, meta)
		dur := time.Since(start).Milliseconds()

		res := &Record{
			APIKeyID:   keyID,
			Tool:       toolName,
			DurationMS: dur,
			InputMeta:  meta.String(),
			Status:     "ok",
		}
		if err != nil {
			res.Status = "error"
			res.ErrorCode = "TOOL_ERROR"
			res.ResultBytes = int64(len(err.Error()))
			rec.submit(*res)
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Result JSON serialization for byte accounting (BR-26).
		b, jerr := json.Marshal(out)
		if jerr != nil {
			res.Status = "error"
			res.ErrorCode = "MARSHAL_ERROR"
			rec.submit(*res)
			return mcp.NewToolResultError("failed to marshal result"), nil
		}
		res.ResultBytes = int64(len(b))
		rec.submit(*res)

		tr, terr := mcp.NewToolResultJSON(out)
		if terr != nil {
			return mcp.NewToolResultError(terr.Error()), nil
		}
		return tr, nil
	}
}

func errorResult(msg string, err error) *mcp.CallToolResult {
	if err != nil {
		msg = fmt.Sprintf("%s: %v", msg, err)
	}
	return mcp.NewToolResultError(msg)
}

// bindArgs unmarshals the request arguments into v.
func bindArgs(req mcp.CallToolRequest, v any) error {
	if req.Params.Arguments == nil {
		// leave zero values
		return nil
	}
	b, err := json.Marshal(req.Params.Arguments)
	if err != nil {
		return errors.New("invalid arguments")
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	return nil
}
