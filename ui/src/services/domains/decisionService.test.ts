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

/**
 * The list is searched and paged on the server, and the views that need every
 * decision read the keys alone. Both used to read full tables — every version,
 * every line, every example — to show a page or draw a graph.
 */
describe("decisionService lists", () => {
  let restore = () => {};
  afterEach(() => restore());

  test("sends a search with the page it asks for, and leaves a blank one out", async () => {
    let sent;
    ({ restore, sent } = stubFetch({ summaries: [], page: { total: 0, page: 2, page_size: 25, has_more: false } }));
    await decisionService.listDecisionSummaries("p1", { page: 2, pageSize: 25, search: " gold " });
    await decisionService.listDecisionSummaries("p1", { page: 1, pageSize: 25, search: "  " });
    expect(new URL(sent[0].url, "http://x").search).toBe("?project_id=p1&page=2&page_size=25&q=gold");
    expect(new URL(sent[1].url, "http://x").search).toBe("?project_id=p1&page=1&page_size=25");
  });

  test("reads the decision keys from their own list, without the tables", async () => {
    let sent;
    ({ restore, sent } = stubFetch({
      summaries: [{ id: "d1", key: "risk", name: "Risk", version: 3, required_decisions: ["score"] }],
      page: { total: 1, page: 1, page_size: 200, has_more: false },
    }));
    const listed = await decisionService.listDecisionSummaries("p1", { page: 1, pageSize: 200 });
    expect(new URL(sent[0].url, "http://x").pathname).toEndWith("/decisions/summaries");
    expect(listed.summaries).toEqual([{ id: "d1", key: "risk", name: "Risk", version: 3, required_decisions: ["score"] }]);
    expect(listed.pageInfo?.total).toBe(1);
  });
});
