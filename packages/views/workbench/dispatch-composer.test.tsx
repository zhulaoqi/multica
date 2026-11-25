import { fireEvent, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithI18n } from "../test/i18n";
import { DispatchComposer } from "./dispatch-composer";

const preview = vi.hoisted(() => ({
  tryOpen: vi.fn(() => true),
}));

vi.mock("../editor", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../editor")>();
  return {
    ...actual,
    useAttachmentPreview: () => ({
      tryOpen: preview.tryOpen,
      open: vi.fn(),
      modal: <div data-testid="attachment-preview" />,
    }),
    useResignedInlineMedia: (_id: string, url: string) => ({
      url,
      pending: false,
    }),
  };
});

function renderComposer(overrides: Partial<React.ComponentProps<typeof DispatchComposer>> = {}) {
  const props: React.ComponentProps<typeof DispatchComposer> = {
    value: "",
    onChange: vi.fn(),
    onSubmit: vi.fn(),
    pending: false,
    onAttachFiles: vi.fn(),
    ...overrides,
  };
  renderWithI18n(<DispatchComposer {...props} />);
  return props;
}

function imageAttachment() {
  return {
    id: "att-image",
    workspace_id: "ws-1",
    issue_id: null,
    comment_id: null,
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: "member",
    uploader_id: "user-1",
    filename: "diagram.png",
    url: "https://files.example/diagram.png",
    download_url: "https://files.example/diagram.png",
    markdown_url: "https://files.example/diagram.png",
    content_type: "image/png",
    size_bytes: 10,
    created_at: "2026-11-25T00:00:00Z",
  };
}

describe("DispatchComposer attachments", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("uploads files pasted into the text box", () => {
    const props = renderComposer();
    const image = new File(["image"], "pasted.png", { type: "image/png" });

    fireEvent.paste(screen.getByRole("textbox"), {
      clipboardData: { files: [image] },
    });

    expect(props.onAttachFiles).toHaveBeenCalledWith([image]);
  });

  it("uploads files dropped anywhere on the composer", () => {
    const props = renderComposer();
    const documentFile = new File(["draft"], "proposal.docx", {
      type: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
    });

    fireEvent.drop(screen.getByTestId("dispatch-composer"), {
      dataTransfer: { files: [documentFile], types: ["Files"], dropEffect: "none" },
    });

    expect(props.onAttachFiles).toHaveBeenCalledWith([documentFile]);
  });

  it("shows an image thumbnail and opens the shared preview", async () => {
    const attachment = imageAttachment();
    renderComposer({ attachments: [attachment] });

    await userEvent.click(
      screen.getByRole("button", { name: "Preview diagram.png" }),
    );

    expect(screen.getByRole("img", { name: "diagram.png" })).toBeInTheDocument();
    expect(preview.tryOpen).toHaveBeenCalledWith({
      kind: "full",
      attachment,
    });
  });
});
