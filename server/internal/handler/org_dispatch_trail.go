package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	// The trail is a summary, not the department's board: a wide plan is cut
	// rather than paged, and progress still counts every child.
	maxTrailChildren      = 20
	trailRootCommentLimit = 50
	trailTreeCommentLimit = 200
)

// dispatchTrailResponse is the requester's view of one dispatched issue tree.
// It carries only the high-level trail (children, their statuses and timing),
// the agent's final answer, and the files those comments produced — never the
// internal comment bodies, descriptions, or tool calls.
type dispatchTrailResponse struct {
	Issue        IssueResponse         `json:"issue"`
	AssigneeName string                `json:"assignee_name,omitempty"`
	Children     []dispatchTrailChild  `json:"children"`
	Progress     dispatchProgress      `json:"progress"`
	Result       *dispatchTrailResult  `json:"result,omitempty"`
	Deliverables []dispatchDeliverable `json:"deliverables"`
}

type dispatchTrailChild struct {
	ID             string  `json:"id"`
	Title          string  `json:"title"`
	Status         string  `json:"status"`
	StatusCategory string  `json:"status_category"`
	StatusLabel    string  `json:"status_label"`
	AssigneeType   string  `json:"assignee_type,omitempty"`
	AssigneeID     string  `json:"assignee_id,omitempty"`
	AssigneeName   string  `json:"assignee_name,omitempty"`
	CreatedAt      string  `json:"created_at"`
	StartedAt      *string `json:"started_at,omitempty"`
	CompletedAt    *string `json:"completed_at,omitempty"`
}

type dispatchTrailResult struct {
	ActorName   string               `json:"actor_name"`
	Content     string               `json:"content"`
	CreatedAt   string               `json:"created_at"`
	Attachments []AttachmentResponse `json:"attachments"`
}

type dispatchDeliverable struct {
	Key          string `json:"key"`
	Filename     string `json:"filename"`
	ContentType  string `json:"content_type"`
	SizeBytes    int64  `json:"size_bytes"`
	VersionCount int    `json:"version_count"`
	DownloadURL  string `json:"download_url"`
	CreatedAt    string `json:"created_at"`
}

// GetDispatchTrail serves the C-end follow-up view of a dispatch that was
// delivered into another workspace. The issue's own workspace events never
// reach the requester's organization socket, so the trail endpoint re-reads
// the tree on demand instead of relying on fan-out.
func (h *Handler) GetDispatchTrail(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	issueUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "issueId"), "issue_id")
	if !ok {
		return
	}
	origin, ok := h.dispatchOriginForRequester(w, r, wsUUID, member.UserID, issueUUID)
	if !ok {
		return
	}
	ctx := r.Context()

	issue, err := h.Queries.GetIssue(ctx, origin.IssueID)
	if err != nil {
		slog.Error("dispatch trail: load issue failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "could not load dispatch")
		return
	}

	children, err := h.Queries.ListChildIssues(ctx, issue.ID)
	if err != nil {
		slog.Error("dispatch trail: load children failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "could not load dispatch trail")
		return
	}

	childIDs := make([]pgtype.UUID, 0, len(children))
	for _, child := range children {
		childIDs = append(childIDs, child.ID)
	}
	taskTimes := make(map[pgtype.UUID]db.ListLatestTaskTimesByIssuesRow, len(childIDs))
	if len(childIDs) > 0 {
		rows, err := h.Queries.ListLatestTaskTimesByIssues(ctx, childIDs)
		if err != nil {
			// Timing is a display hint; the trail stays usable without it.
			slog.Warn("dispatch trail: load task times failed", append(logger.RequestAttrs(r), "error", err)...)
		}
		for _, row := range rows {
			taskTimes[row.IssueID] = row
		}
	}

	labels := h.dispatchStatusLabels(ctx, issue.WorkspaceID)
	resolver := issuestatus.NewResolver(issue.WorkspaceID)
	nameCache := make(map[string]string)
	visible := children
	if len(visible) > maxTrailChildren {
		visible = visible[:maxTrailChildren]
	}
	childRows := make([]dispatchTrailChild, 0, len(visible))
	for _, child := range visible {
		row := dispatchTrailChild{
			ID:             uuidToString(child.ID),
			Title:          child.Title,
			Status:         child.Status,
			StatusCategory: issuestatus.WireCategory(child.Status, resolver.Category(ctx, h.Queries, child.Status)),
			StatusLabel:    dispatchStatusLabel(labels, child.Status),
			AssigneeType:   child.AssigneeType.String,
			AssigneeID:     uuidToString(child.AssigneeID),
			CreatedAt:      timestampToString(child.CreatedAt),
		}
		row.AssigneeName = h.trailActorName(ctx, issue.WorkspaceID, row.AssigneeType, row.AssigneeID, nameCache)
		if times, ok := taskTimes[child.ID]; ok {
			row.StartedAt = timestampToPtr(times.StartedAt)
			row.CompletedAt = timestampToPtr(times.CompletedAt)
		}
		childRows = append(childRows, row)
	}

	progress := dispatchProgress{Total: int64(len(children))}
	if len(children) > 0 {
		terminal := make(map[string]bool)
		keys, err := h.terminalIssueStatusKeys(ctx, issue.WorkspaceID)
		if err != nil {
			// Falling back to the built-in terminal keys keeps progress right for
			// the common case rather than hiding it on a catalog hiccup.
			terminal[issuestatus.Done] = true
			terminal[issuestatus.Cancelled] = true
		}
		for _, key := range keys {
			terminal[key] = true
		}
		for _, child := range children {
			if terminal[child.Status] {
				progress.Done++
			}
		}
	}

	prefix := h.getIssuePrefix(ctx, issue.WorkspaceID)
	issueResp := issueToResponse(issue, prefix)
	h.fillStatusCategory(ctx, issue.WorkspaceID, &issueResp)
	resp := dispatchTrailResponse{
		Issue:        issueResp,
		AssigneeName: h.trailActorName(ctx, issue.WorkspaceID, issue.AssigneeType.String, uuidToString(issue.AssigneeID), nameCache),
		Children:     childRows,
		Progress:     progress,
	}

	result, err := h.dispatchTrailResult(ctx, r, issue)
	if err != nil {
		slog.Warn("dispatch trail: load result failed", append(logger.RequestAttrs(r), "error", err)...)
	}
	resp.Result = result

	deliverables, err := h.dispatchTrailDeliverables(ctx, r, issue, childIDs)
	if err != nil {
		slog.Warn("dispatch trail: load deliverables failed", append(logger.RequestAttrs(r), "error", err)...)
	}
	if deliverables == nil {
		deliverables = []dispatchDeliverable{}
	}
	resp.Deliverables = deliverables

	writeJSON(w, http.StatusOK, resp)
}

// dispatchOriginForRequester loads the origin and enforces that the caller is
// the person who asked for the work. Missing and foreign dispatches answer the
// same 404 so the endpoint cannot be used to probe issue ids.
func (h *Handler) dispatchOriginForRequester(w http.ResponseWriter, r *http.Request, wsUUID, userID, issueID pgtype.UUID) (db.DispatchOrigin, bool) {
	origin, err := h.Queries.GetDispatchOrigin(r.Context(), issueID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErrorCode(w, http.StatusNotFound, "dispatch_not_found", "dispatch not found")
		return db.DispatchOrigin{}, false
	}
	if err != nil {
		slog.Error("dispatch trail: load origin failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "could not load dispatch")
		return db.DispatchOrigin{}, false
	}
	if origin.RequesterID != userID || origin.WorkspaceID != wsUUID {
		writeErrorCode(w, http.StatusNotFound, "dispatch_not_found", "dispatch not found")
		return db.DispatchOrigin{}, false
	}
	return origin, true
}

// DeleteDispatch removes one thread from the requester's history. The issue,
// its comments and the work delivered in its workspace are untouched; only the
// origin row that lists the thread goes, so follow-ups and notices stop too.
func (h *Handler) DeleteDispatch(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	issueUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "issueId"), "issue_id")
	if !ok {
		return
	}
	if _, ok := h.dispatchOriginForRequester(w, r, wsUUID, member.UserID, issueUUID); !ok {
		return
	}
	deleted, err := h.Queries.DeleteDispatchOrigin(r.Context(), db.DeleteDispatchOriginParams{
		IssueID:     issueUUID,
		WorkspaceID: wsUUID,
		RequesterID: member.UserID,
	})
	if err != nil {
		slog.Error("delete dispatch: remove origin failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "could not delete the dispatch")
		return
	}
	if deleted == 0 {
		writeErrorCode(w, http.StatusNotFound, "dispatch_not_found", "dispatch not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// dispatchStatusLabels reads the catalog once so custom statuses resolve to the
// workspace's own name. Archived entries stay in the map because old issues may
// still carry them.
func (h *Handler) dispatchStatusLabels(ctx context.Context, workspaceID pgtype.UUID) map[string]string {
	entries, err := h.Queries.ListIssueStatusEntries(ctx, db.ListIssueStatusEntriesParams{
		WorkspaceID:     workspaceID,
		IncludeArchived: true,
	})
	if err != nil {
		return nil
	}
	labels := make(map[string]string, len(entries))
	for _, entry := range entries {
		labels[entry.Key] = entry.Name
	}
	return labels
}

func dispatchStatusLabel(labels map[string]string, status string) string {
	if name, ok := labels[status]; ok && name != "" {
		return name
	}
	return status
}

// trailActorName resolves an assignee for the summary rows, memoized: a plan
// usually names a handful of agents even when it has many steps.
func (h *Handler) trailActorName(ctx context.Context, workspaceID pgtype.UUID, actorType, actorID string, cache map[string]string) string {
	if actorType == "" || actorID == "" {
		return ""
	}
	key := actorType + ":" + actorID
	if name, ok := cache[key]; ok {
		return name
	}
	name := ""
	if id, err := util.ParseUUID(actorID); err == nil {
		if resolved := h.actorDisplayName(ctx, workspaceID, actorType, id); resolved != actorType {
			name = resolved
		}
	}
	cache[key] = name
	return name
}

// dispatchTrailResult returns the newest agent comment on the root issue — the
// answer the department delivered — with its files signed for direct download.
func (h *Handler) dispatchTrailResult(ctx context.Context, r *http.Request, issue db.Issue) (*dispatchTrailResult, error) {
	comments, err := h.Queries.ListCommentsForIssue(ctx, db.ListCommentsForIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		Limit:       trailRootCommentLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("list root comments: %w", err)
	}
	var latest *db.Comment
	for i := range comments {
		comment := comments[i]
		if comment.Type != "comment" || comment.DeletedAt.Valid || comment.AuthorType != "agent" {
			continue
		}
		if strings.TrimSpace(comment.Content) == "" {
			continue
		}
		latest = &comments[i]
	}
	if latest == nil {
		return nil, nil
	}

	actorName := h.trailActorName(ctx, issue.WorkspaceID, latest.AuthorType, uuidToString(latest.AuthorID), map[string]string{})
	attachments, err := h.Queries.ListAttachmentsByCommentIDs(ctx, db.ListAttachmentsByCommentIDsParams{
		Column1:     []pgtype.UUID{latest.ID},
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		return nil, fmt.Errorf("list result attachments: %w", err)
	}
	mode := attachmentURLModeFromRequest(r)
	files := make([]AttachmentResponse, 0, len(attachments))
	for _, attachment := range attachments {
		files = append(files, h.attachmentToResponse(attachment, mode))
	}
	return &dispatchTrailResult{
		ActorName:   actorName,
		Content:     latest.Content,
		CreatedAt:   timestampToString(latest.CreatedAt),
		Attachments: files,
	}, nil
}

// dispatchTrailDeliverables folds every file uploaded in the tree's comments
// by (content type, filename), the same identity the web client uses, and
// returns the newest upload of each file.
func (h *Handler) dispatchTrailDeliverables(ctx context.Context, r *http.Request, issue db.Issue, childIDs []pgtype.UUID) ([]dispatchDeliverable, error) {
	issueIDs := make([]pgtype.UUID, 0, len(childIDs)+1)
	issueIDs = append(issueIDs, issue.ID)
	issueIDs = append(issueIDs, childIDs...)
	comments, err := h.Queries.ListCommentsForIssueTree(ctx, db.ListCommentsForIssueTreeParams{
		IssueIds:    issueIDs,
		WorkspaceID: issue.WorkspaceID,
		RowLimit:    trailTreeCommentLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("list tree comments: %w", err)
	}
	commentIDs := make([]pgtype.UUID, 0, len(comments))
	for _, comment := range comments {
		commentIDs = append(commentIDs, comment.ID)
	}
	if len(commentIDs) == 0 {
		return nil, nil
	}
	attachments, err := h.Queries.ListAttachmentsByCommentIDs(ctx, db.ListAttachmentsByCommentIDsParams{
		Column1:     commentIDs,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		return nil, fmt.Errorf("list tree attachments: %w", err)
	}

	mode := attachmentURLModeFromRequest(r)
	type deliverableState struct {
		latest   db.Attachment
		versions int
	}
	byKey := make(map[string]*deliverableState)
	order := make([]string, 0, len(attachments))
	for _, attachment := range attachments {
		key := deliverableKey(attachment)
		state, ok := byKey[key]
		if !ok {
			byKey[key] = &deliverableState{latest: attachment, versions: 1}
			order = append(order, key)
			continue
		}
		state.versions++
		if newerAttachment(attachment, state.latest) {
			state.latest = attachment
		}
	}

	files := make([]dispatchDeliverable, 0, len(byKey))
	for _, key := range order {
		state := byKey[key]
		files = append(files, dispatchDeliverable{
			Key:          key,
			Filename:     state.latest.Filename,
			ContentType:  state.latest.ContentType,
			SizeBytes:    state.latest.SizeBytes,
			VersionCount: state.versions,
			DownloadURL:  h.attachmentToResponse(state.latest, mode).DownloadURL,
			CreatedAt:    timestampToString(state.latest.CreatedAt),
		})
	}
	sort.SliceStable(files, func(i, j int) bool {
		if files[i].CreatedAt != files[j].CreatedAt {
			return files[i].CreatedAt > files[j].CreatedAt
		}
		return files[i].Key > files[j].Key
	})
	return files, nil
}

// deliverableKey mirrors the web client's deliverable identity: the content
// type without parameters plus the trimmed filename.
func deliverableKey(attachment db.Attachment) string {
	contentType := strings.ToLower(strings.TrimSpace(attachment.ContentType))
	if semi := strings.IndexByte(contentType, ';'); semi >= 0 {
		contentType = strings.TrimSpace(contentType[:semi])
	}
	return contentType + "\x00" + strings.TrimSpace(attachment.Filename)
}

// newerAttachment reports whether a was uploaded after b, breaking a created_at
// tie by id (UUIDv7, so id order is upload order).
func newerAttachment(a, b db.Attachment) bool {
	if !a.CreatedAt.Time.Equal(b.CreatedAt.Time) {
		return a.CreatedAt.Time.After(b.CreatedAt.Time)
	}
	return uuidToString(a.ID) > uuidToString(b.ID)
}

type dispatchMessageRequest struct {
	Content       string   `json:"content"`
	AttachmentIDs []string `json:"attachment_ids"`
}

// CommentDispatch posts the requester's follow-up into the issue's own
// workspace and wakes its executor.
func (h *Handler) CommentDispatch(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	issueUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "issueId"), "issue_id")
	if !ok {
		return
	}
	var body dispatchMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	attachmentIDs, ok := parseUUIDSliceOrBadRequest(w, body.AttachmentIDs, "attachment_ids")
	if !ok {
		return
	}
	reply, err := h.OrgDispatchService.CommentChatDispatch(
		r.Context(),
		member.UserID,
		issueUUID,
		body.Content,
		attachmentIDs,
	)
	if err != nil {
		h.writeDispatchControlError(w, r, err)
		return
	}
	resp := issueToResponse(reply.Issue, h.getIssuePrefix(r.Context(), reply.Issue.WorkspaceID))
	h.fillStatusCategory(r.Context(), reply.Issue.WorkspaceID, &resp)
	writeJSON(w, http.StatusOK, map[string]any{
		"issue":     resp,
		"delivered": reply.Delivered,
	})
}

type dispatchStatusRequest struct {
	Status string `json:"status"`
}

// SetDispatchStatus accepts or sends back a delivered dispatch from the web
// workbench. It is the requester's verdict, not a general issue edit.
func (h *Handler) SetDispatchStatus(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	issueUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "issueId"), "issue_id")
	if !ok {
		return
	}
	var body dispatchStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	issue, err := h.OrgDispatchService.UpdateChatDispatchStatus(r.Context(), member.UserID, issueUUID, strings.TrimSpace(body.Status))
	if err != nil {
		h.writeDispatchControlError(w, r, err)
		return
	}
	resp := issueToResponse(issue, h.getIssuePrefix(r.Context(), issue.WorkspaceID))
	h.fillStatusCategory(r.Context(), issue.WorkspaceID, &resp)
	writeJSON(w, http.StatusOK, map[string]any{"issue": resp})
}

// writeDispatchControlError maps the requester control surface's failures.
// Missing, foreign, and closed dispatches never reveal whether an issue exists.
func (h *Handler) writeDispatchControlError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrChatDispatchNotFound), errors.Is(err, service.ErrChatDispatchNotOwner):
		writeErrorCode(w, http.StatusNotFound, "dispatch_not_found", "dispatch not found")
	case errors.Is(err, service.ErrChatDispatchClosed):
		writeErrorCode(w, http.StatusConflict, "dispatch_closed", "dispatch is already closed")
	case errors.Is(err, service.ErrChatDispatchInvalidStatus):
		writeErrorCode(w, http.StatusBadRequest, "dispatch_invalid_status", "status must be done or in_progress")
	case errors.Is(err, service.ErrDispatchTextRequired):
		writeErrorCode(w, http.StatusBadRequest, "dispatch_text_required", "text is required")
	case errors.Is(err, service.ErrDispatchAttachmentNotFound):
		writeErrorCode(w, http.StatusBadRequest, "attachment_not_found", "some attachments are not available to this request")
	default:
		slog.Error("dispatch control failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "could not update dispatch")
	}
}
