import assert from "node:assert/strict";
import test from "node:test";
import { createAxiosMutator } from "../src/shared/api/createAxiosMutator.ts";

function recordingClient() {
  const requests: unknown[] = [];
  return {
    requests,
    request(config: unknown) {
      requests.push(config);
      return Promise.resolve({ data: { status: "ok" } });
    },
  };
}

test("generated API requests retain Axios options and use the session API instance", async () => {
  const api = recordingClient();
  const http = recordingClient();
  const mutator = createAxiosMutator(api as never, http as never);
  const controller = new AbortController();
  const headers = { "X-Request-Id": "fixture" };
  const result = await mutator(
    {
      url: "/api/admin/status/",
      method: "GET",
      headers,
      timeout: 1250,
      signal: controller.signal,
    },
  );

  assert.deepEqual(result, { status: "ok" });
  assert.equal(api.requests.length, 1);
  assert.equal(http.requests.length, 0);
  assert.deepEqual(api.requests[0], {
    url: "/admin/status/",
    method: "GET",
    headers,
    timeout: 1250,
    signal: controller.signal,
  });
});

test("flag receiver requests use the root HTTP client and retain cancellation", async () => {
  const api = recordingClient();
  const http = recordingClient();
  const mutator = createAxiosMutator(api as never, http as never);
  const controller = new AbortController();
  const headers = { "X-Team-Token": "fixture-token" };

  await mutator({
    url: "/flags/",
    method: "PUT",
    headers,
    data: ["fixture-flag"],
    signal: controller.signal,
  });

  assert.equal(api.requests.length, 0);
  assert.deepEqual(http.requests[0], {
    url: "/flags/",
    method: "PUT",
    headers,
    data: ["fixture-flag"],
    signal: controller.signal,
  });
});
