// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  customRuntimeDocsHref,
  daemonRuntimesDocsHref,
} from "./runtime-docs";

describe("runtime docs links", () => {
  it.each([
    ["en", "/docs/daemon-runtimes"],
    ["zh-Hans", "/docs/zh/daemon-runtimes"],
    ["zh-Hant", "/docs/zh-Hant/daemon-runtimes"],
    ["ja", "/docs/ja/daemon-runtimes"],
    ["ko", "/docs/ko/daemon-runtimes"],
    ["fr", "/docs/fr/daemon-runtimes"],
  ])("localizes the daemon guide for %s", (language, expected) => {
    expect(daemonRuntimesDocsHref(language)).toBe(expected);
  });

  it("adds the localized custom runtime section", () => {
    expect(customRuntimeDocsHref("zh-Hans")).toBe(
      `/docs/zh/daemon-runtimes#${encodeURIComponent("自定义运行时配置")}`,
    );
    expect(customRuntimeDocsHref("zh-Hant")).toBe(
      `/docs/zh-Hant/daemon-runtimes#${encodeURIComponent("自定義執行時配置")}`,
    );
    expect(customRuntimeDocsHref("fr")).toBe(
      `/docs/fr/daemon-runtimes#${encodeURIComponent("profils-de-runtime-personnalisés")}`,
    );
  });
});
