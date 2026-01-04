package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"team-assistant/internal/model"
	"team-assistant/internal/svc"
)

// Response 通用响应
type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(Response{
		Code:    code,
		Message: message,
	})
}

func writeSuccess(w http.ResponseWriter, data interface{}) {
	writeJSON(w, Response{
		Code:    0,
		Message: "success",
		Data:    data,
	})
}

// StatsHandler 统计数据处理器
type StatsHandler struct {
	svcCtx *svc.ServiceContext
}

// NewStatsHandler 创建统计处理器
func NewStatsHandler(svcCtx *svc.ServiceContext) *StatsHandler {
	return &StatsHandler{svcCtx: svcCtx}
}

// Handle 处理统计请求
func (h *StatsHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx := context.Background()

	// 解析时间范围
	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	var startTime, endTime time.Time
	var err error

	if startStr != "" {
		startTime, err = time.Parse("2006-01-02", startStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Invalid start date format")
			return
		}
	} else {
		// 默认7天前
		startTime = time.Now().AddDate(0, 0, -7)
	}

	if endStr != "" {
		endTime, err = time.Parse("2006-01-02", endStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Invalid end date format")
			return
		}
	} else {
		endTime = time.Now()
	}

	// 获取统计数据
	stats, err := h.svcCtx.CommitModel.GetAllStats(ctx, startTime, endTime)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to get stats")
		return
	}

	writeSuccess(w, map[string]interface{}{
		"start_time": startTime.Format("2006-01-02"),
		"end_time":   endTime.Format("2006-01-02"),
		"stats":      stats,
	})
}

// MemberHandler 成员管理处理器
type MemberHandler struct {
	svcCtx *svc.ServiceContext
}

// NewMemberHandler 创建成员处理器
func NewMemberHandler(svcCtx *svc.ServiceContext) *MemberHandler {
	return &MemberHandler{svcCtx: svcCtx}
}

// Handle 处理成员请求
func (h *MemberHandler) Handle(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listMembers(w, r)
	case http.MethodPost:
		h.addMember(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (h *MemberHandler) listMembers(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	members, err := h.svcCtx.MemberModel.ListAll(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to list members")
		return
	}

	writeSuccess(w, members)
}

func (h *MemberHandler) addMember(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()

	var req struct {
		Name           string `json:"name"`
		GitHubUsername string `json:"github_username"`
		LarkUserID     string `json:"lark_user_id"`
		LarkOpenID     string `json:"lark_open_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "Name is required")
		return
	}

	member := &model.TeamMember{
		Name:           req.Name,
		GitHubUsername: sql.NullString{String: req.GitHubUsername, Valid: req.GitHubUsername != ""},
		LarkUserID:     sql.NullString{String: req.LarkUserID, Valid: req.LarkUserID != ""},
		LarkOpenID:     sql.NullString{String: req.LarkOpenID, Valid: req.LarkOpenID != ""},
	}

	if err := h.svcCtx.MemberModel.Upsert(ctx, member); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to add member")
		return
	}

	writeSuccess(w, member)
}
