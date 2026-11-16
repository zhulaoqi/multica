import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { issueKeys } from "../issues/queries";
import { createSafeId } from "../utils";
import { workbenchKeys } from "./queries";
import type {
  DispatchListItem,
  DispatchWorkInput,
  DispatchWorkResponse,
} from "./schemas";

export interface DispatchWorkVariables {
  text: string;
  unitId?: string;
  clientRequestId?: string;
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
      return api.dispatchWork(input);
    },
    onSuccess: (data) => {
      // Show the new thread immediately; the invalidation below replaces this
      // provisional row with the server's leader_state as it changes.
      const item: DispatchListItem = {
        issue: data.issue,
        unit_id: data.dispatch.unit_id,
        unit_path: data.dispatch.unit_path,
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
