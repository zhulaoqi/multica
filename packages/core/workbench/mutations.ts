import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { issueKeys } from "../issues/queries";
import { createSafeId } from "../utils";
import { workbenchKeys } from "./queries";
import type {
  DispatchCommentResponse,
  DispatchListItem,
  DispatchStatusResponse,
  DispatchWorkInput,
  DispatchWorkResponse,
} from "./schemas";

export interface DispatchWorkVariables {
  text: string;
  unitId?: string;
  /** Target a project: the work is created and run inside that project's workspace. */
  projectId?: string;
  personal?: boolean;
  clientRequestId?: string;
  attachmentIds?: string[];
}

export function useDispatchWork(wsId: string) {
  const qc = useQueryClient();
  return useMutation<DispatchWorkResponse, unknown, DispatchWorkVariables>({
    mutationFn: (variables) => {
      const input: DispatchWorkInput = {
        text: variables.text,
        client_request_id: variables.clientRequestId ?? createSafeId(),
      };
      if (variables.unitId) input.unit_id = variables.unitId;
      if (variables.projectId) input.project_id = variables.projectId;
      if (variables.personal) input.personal = true;
      if (variables.attachmentIds?.length) input.attachment_ids = variables.attachmentIds;
      return api.dispatchWork(input);
    },
    onSuccess: (data) => {
      // Show the new thread immediately; the invalidation below replaces this
      // provisional row with the server's leader_state as it changes.
      const item: DispatchListItem = {
        issue: data.issue,
        request_attachments: [],
        unit_id: data.dispatch.unit_id,
        unit_path: data.dispatch.unit_path,
        squad_id: data.dispatch.squad_id,
        personal: data.dispatch.personal,
        project_id: data.dispatch.project_id,
        project_name: data.dispatch.project_name,
        dispatched_at: data.issue.created_at,
        completed: false,
        leader_state: "queued",
      };
      qc.setQueryData<DispatchListItem[]>(
        workbenchKeys.dispatches(wsId),
        (previous) => {
          if (!previous) return previous;
          if (previous.some((entry) => entry.issue.id === item.issue.id)) {
            return previous;
          }
          return [item, ...previous];
        },
      );
      qc.invalidateQueries({ queryKey: workbenchKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.childProgress(wsId) });
    },
  });
}

export interface CommentDispatchVariables {
  content: string;
  attachmentIds?: string[];
}

/**
 * Posts the requester's follow-up into the dispatch's own workspace. The
 * server writes the comment there and wakes the executor, so the response
 * carries the updated issue rather than the whole trail.
 */
export function useCommentDispatch(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation<DispatchCommentResponse, unknown, CommentDispatchVariables>({
    mutationFn: ({ content, attachmentIds }) =>
      api.commentDispatch(issueId, content, attachmentIds),
    onSuccess: (data) => {
      qc.setQueryData<DispatchListItem[]>(
        workbenchKeys.dispatches(wsId),
        (previous) =>
          previous?.map((item) =>
            item.issue.id === data.issue.id ? { ...item, issue: data.issue } : item,
          ),
      );
      qc.invalidateQueries({ queryKey: workbenchKeys.trail(wsId, issueId) });
      qc.invalidateQueries({ queryKey: workbenchKeys.dispatches(wsId) });
    },
  });
}

export interface SetDispatchStatusVariables {
  issueId: string;
  status: "done" | "in_progress";
}

/**
 * The requester's verdict: accepting closes the root, sending it back returns
 * it to in progress. Both wait for the server so a failed write keeps the
 * requester on the same screen with the error shown.
 */
export function useSetDispatchStatus(wsId: string) {
  const qc = useQueryClient();
  return useMutation<DispatchStatusResponse, unknown, SetDispatchStatusVariables>({
    mutationFn: ({ issueId, status }) => api.setDispatchStatus(issueId, status),
    onSuccess: (data, variables) => {
      qc.setQueryData<DispatchListItem[]>(
        workbenchKeys.dispatches(wsId),
        (previous) =>
          previous?.map((item) =>
            item.issue.id === data.issue.id ? { ...item, issue: data.issue } : item,
          ),
      );
      qc.invalidateQueries({ queryKey: workbenchKeys.trail(wsId, variables.issueId) });
      qc.invalidateQueries({ queryKey: workbenchKeys.dispatches(wsId) });
    },
  });
}

export interface ReassignDispatchVariables {
  issueId: string;
  unitId: string;
}

/**
 * Moves a dispatch the caller requested to another department. The server
 * clears the previous delivery notices and wakes the new leader, so the
 * cached row is reset to a queued state until the refetch confirms it.
 */
export function useReassignDispatch(wsId: string) {
  const qc = useQueryClient();
  return useMutation<DispatchWorkResponse, unknown, ReassignDispatchVariables>({
    mutationFn: ({ issueId, unitId }) => api.reassignDispatch(issueId, unitId),
    onSuccess: (data) => {
      qc.setQueryData<DispatchListItem[]>(
        workbenchKeys.dispatches(wsId),
        (previous) =>
          previous?.map((item) =>
            item.issue.id === data.issue.id
              ? {
                  ...item,
                  issue: data.issue,
                  unit_id: data.dispatch.unit_id,
                  unit_path: data.dispatch.unit_path,
                  squad_id: data.dispatch.squad_id,
                  personal: data.dispatch.personal,
                  // Reassignment drops the project: the row now answers only
                  // the new department.
                  project_id: "",
                  project_name: "",
                  completed: false,
                  notified_at: null,
                  leader_state: "queued",
                  blocker: null,
                }
              : item,
          ),
      );
      qc.invalidateQueries({ queryKey: workbenchKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.detail(wsId, data.issue.id) });
      qc.invalidateQueries({ queryKey: issueKeys.childProgress(wsId) });
    },
  });
}

/**
 * Removes one thread from the requester's history. The server waits before
 * answering, so the row stays visible with the dialog's pending state until
 * the list invalidation drops it.
 */
export function useDeleteDispatch(wsId: string) {
  const qc = useQueryClient();
  return useMutation<void, unknown, string>({
    mutationFn: (issueId) => api.deleteDispatch(issueId),
    onSuccess: (_data, issueId) => {
      qc.setQueryData<DispatchListItem[]>(
        workbenchKeys.dispatches(wsId),
        (previous) => previous?.filter((item) => item.issue.id !== issueId),
      );
      qc.removeQueries({ queryKey: workbenchKeys.trail(wsId, issueId) });
      qc.invalidateQueries({ queryKey: workbenchKeys.dispatches(wsId) });
    },
  });
}
