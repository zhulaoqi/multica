// @vitest-environment node
import { describe, expect, it } from "vitest";
import { Reservations } from "./reservations";

describe("Reservations", () => {
  const reservations = () =>
    new Reservations([
      { key: "coffee", capacity: 1 },
      { key: "sofa", capacity: 3 },
    ]);

  it("gives an exclusive station to one owner", () => {
    const table = reservations();
    expect(table.tryAcquire("coffee", "a")).toBe(true);
    expect(table.isFree("coffee")).toBe(false);
    expect(table.tryAcquire("coffee", "b")).toBe(false);
    expect(table.holds("coffee", "a")).toBe(true);
  });

  it("counts capacity for shared stations", () => {
    const table = reservations();
    expect(table.tryAcquire("sofa", "a")).toBe(true);
    expect(table.tryAcquire("sofa", "b")).toBe(true);
    expect(table.tryAcquire("sofa", "c")).toBe(true);
    expect(table.tryAcquire("sofa", "d")).toBe(false);
    expect(table.isFree("sofa")).toBe(false);
  });

  it("is idempotent for the same owner and frees on release", () => {
    const table = reservations();
    expect(table.tryAcquire("coffee", "a")).toBe(true);
    expect(table.tryAcquire("coffee", "a")).toBe(true);
    table.release("coffee", "a");
    expect(table.isFree("coffee")).toBe(true);
  });

  it("frees everything an owner held when it leaves", () => {
    const table = reservations();
    table.tryAcquire("coffee", "a");
    table.tryAcquire("sofa", "a");
    table.releaseAll("a");
    expect(table.isFree("coffee")).toBe(true);
    expect(table.isFree("sofa")).toBe(true);
  });

  it("treats unknown stations as unavailable", () => {
    const table = reservations();
    expect(table.isFree("nope")).toBe(false);
    expect(table.tryAcquire("nope", "a")).toBe(false);
  });
});
