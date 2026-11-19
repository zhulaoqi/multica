package main

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"path"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/integrations/lark"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// orgBotAttachmentStore persists files a requester sends into a dispatch
// thread. It mirrors the issue-upload path in the file handler: the object goes
// to storage under the workspace prefix, the attachment row carries the issue,
// and the revision bump is published so open clients refetch.
type orgBotAttachmentStore struct {
	handler *handler.Handler
}

var _ lark.OrgBotAttachmentStore = orgBotAttachmentStore{}

func (s orgBotAttachmentStore) AttachToIssue(ctx context.Context, attachment lark.OrgBotAttachment) (lark.OrgBotAttachmentResult, error) {
	h := s.handler
	if h == nil || h.Queries == nil || h.Storage == nil {
		return lark.OrgBotAttachmentResult{}, errors.New("attachment storage is not configured")
	}
	if len(attachment.Data) == 0 {
		return lark.OrgBotAttachmentResult{}, errors.New("attachment is empty")
	}
	issue, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          attachment.IssueID,
		WorkspaceID: attachment.WorkspaceID,
	})
	if err != nil {
		return lark.OrgBotAttachmentResult{}, fmt.Errorf("load attachment issue: %w", err)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return lark.OrgBotAttachmentResult{}, fmt.Errorf("generate attachment id: %w", err)
	}
	contentType := strings.TrimSpace(attachment.ContentType)
	if contentType == "" {
		contentType = mime.TypeByExtension(path.Ext(attachment.Filename))
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	key := "workspaces/" + util.UUIDToString(attachment.WorkspaceID) + "/" + id.String() + path.Ext(attachment.Filename)

	link, err := h.Storage.Upload(ctx, key, attachment.Data, contentType, attachment.Filename)
	if err != nil {
		return lark.OrgBotAttachmentResult{}, fmt.Errorf("upload attachment: %w", err)
	}
	att, err := h.Queries.CreateAttachment(ctx, db.CreateAttachmentParams{
		ID:           pgtype.UUID{Bytes: id, Valid: true},
		WorkspaceID:  attachment.WorkspaceID,
		UploaderType: "member",
		UploaderID:   attachment.ActorID,
		Filename:     attachment.Filename,
		Url:          link,
		ContentType:  contentType,
		SizeBytes:    int64(len(attachment.Data)),
		IssueID:      attachment.IssueID,
	})
	if err != nil {
		h.Storage.Delete(ctx, key)
		return lark.OrgBotAttachmentResult{}, fmt.Errorf("create attachment row: %w", err)
	}
	if att.IssueRevision > 0 && att.IssueID.Valid && h.IssueService != nil {
		h.IssueService.PublishAttachmentsChanged(ctx, issue, attachment.ActorID)
	}
	return lark.OrgBotAttachmentResult{Filename: attachment.Filename, SizeBytes: int64(len(attachment.Data))}, nil
}
