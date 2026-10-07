import { beforeEach, describe, expect, it, vi } from "vitest";
import { shouldReauth } from "./reauth";

beforeEach(() => sessionStorage.clear());

describe("shouldReauth rate limit", () => {
  it("allows the first login, blocks repeats within 30s, allows after", () => {
    expect(shouldReauth(100_000)).toBe(true);
    expect(shouldReauth(110_000)).toBe(false);
    expect(shouldReauth(130_001)).toBe(true);
  });
  it("still allows when storage throws", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("denied");
    });
    expect(shouldReauth(100_000)).toBe(true);
    vi.restoreAllMocks();
  });
});
