import { afterEach, describe, expect, test } from "bun:test";
import { decisionService } from "./decisionService";
import { stubFetch } from "../shared/stubbedFetch";

describe("decisionService.createDecision", () => {
  let restore = () => {};
  afterEach(() => restore());

  test("raises the refusal instead of returning it", async () => {
    ({ restore } = stubFetch({ err: "a decision with this key exists" }));
    await expect(
      decisionService.createDecision({ key: "approval-level", name: "Approval level", version: 1, inputs: [], outputs: [], rules: [] }),
    ).rejects.toThrow("a decision with this key exists");
  });
});

/**
 * A position is only meaningful against the stored table that produced it; an
 * id survives lines being moved or added. The server sends both, and the id
 * was dropped here, so the editor could only highlight by position.
 */
describe("decisionService.evaluateDecision", () => {
  let restore = () => {};
  afterEach(() => restore());

  test("passes on which lines decided, by id as well as by position", async () => {
    ({ restore } = stubFetch({ result: { values: { band: "HIGH" }, matched_rules: [2], matched_rule_ids: ["r-high"] } }));
    const response = await decisionService.evaluateDecision("p1", "band", { amount: 500 }, 3);
    expect(response.matchedRules).toEqual([2]);
    expect(response.matchedRuleIds).toEqual(["r-high"]);
  });

  test("asks for the version it was given", async () => {
    let sent;
    ({ restore, sent } = stubFetch({ result: { values: {} } }));
    await decisionService.evaluateDecision("p1", "band", {}, 3);
    expect(sent[0].body).toEqual({ project_id: "p1", key: "band", variables: {}, version: 3 });
  });
});
