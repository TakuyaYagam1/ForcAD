import assert from "node:assert/strict";
import test from "node:test";
import { AxiosHeaders } from "axios";
import type { AxiosRequestConfig, RawAxiosHeaders } from "axios";
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

const firstToken = "a".repeat(64);
const secondToken = "b".repeat(64);

function sessionClient(respond?: (config: AxiosRequestConfig) => Promise<unknown>) {
  const requests: AxiosRequestConfig[] = [];
  return {
    requests,
    defaults: { baseURL: "https://forcad.invalid/api" },
    async request(config: AxiosRequestConfig) {
      requests.push(config);
      return { data: respond ? await respond(config) : { csrf_token: firstToken } };
    },
  };
}

function tokenIn(request: AxiosRequestConfig) {
  return AxiosHeaders.from(request.headers as RawAxiosHeaders | AxiosHeaders | undefined).get("X-CSRF-Token");
}

test("login token is retained only for admin mutations and logout invalidates it", async () => {
  const api = sessionClient();
  const http = sessionClient();
  const mutate = createAxiosMutator(api as never, http as never);
  await mutate({ url: "/api/admin/login/", method: "POST", data: { username: "fixture" } });
  await mutate({ url: "/api/admin/teams/1/", method: "PATCH", data: { name: "fixture" } });
  await mutate({ url: "/api/admin/logout/", method: "POST" });
  await mutate({ url: "/api/admin/game/pause/", method: "POST" });
  assert.deepEqual(api.requests.map((request) => request.url), [
    "/admin/login/", "/admin/teams/1/", "/admin/logout/", "/admin/status/", "/admin/game/pause/",
  ]);
  assert.equal(tokenIn(api.requests[0]), undefined);
  assert.equal(tokenIn(api.requests[1]), firstToken);
  assert.equal(tokenIn(api.requests[2]), firstToken);
  assert.equal(tokenIn(api.requests[3]), undefined);
  assert.equal(tokenIn(api.requests[4]), firstToken);
});

test("parallel mutations after reload share one status request", async () => {
  const api = sessionClient();
  const mutate = createAxiosMutator(api as never, sessionClient() as never);
  await Promise.all([
    mutate({ url: "/api/admin/tasks/1/", method: "PUT" }),
    mutate({ url: "/api/admin/teams/1/", method: "DELETE" }),
  ]);
  assert.equal(api.requests.filter(({ url }) => url === "/admin/status/").length, 1);
  assert.equal(api.requests.length, 3);
  for (const request of api.requests.slice(1)) assert.equal(tokenIn(request), firstToken);
});

test("missing session token prevents mutations from being sent", async () => {
  const api = sessionClient(async () => ({ username: "fixture" }));
  const mutate = createAxiosMutator(api as never, sessionClient() as never);
  await assert.rejects(mutate({ url: "/api/admin/events/", method: "POST" }), /no CSRF token/);
  assert.deepEqual(api.requests.map(({ url }) => url), ["/admin/status/"]);
});

test("403 clears cached token without replaying a mutation", async () => {
  let fail = true;
  const api = sessionClient(async ({ url }) => {
    if (url === "/admin/status/") return { csrf_token: fail ? firstToken : secondToken };
    if (fail) throw { response: { status: 403 } };
    return { status: "ok" };
  });
  const mutate = createAxiosMutator(api as never, sessionClient() as never);
  await assert.rejects(mutate({ url: "/api/admin/game/pause/", method: "POST" }));
  assert.equal(api.requests.length, 2);
  fail = false;
  await mutate({ url: "/api/admin/game/pause/", method: "POST" });
  assert.equal(api.requests.length, 4);
  assert.equal(tokenIn(api.requests[3]), secondToken);
});

test("CSRF headers never accompany public, external, or safe requests", async () => {
  const api = sessionClient();
  const http = sessionClient();
  const mutate = createAxiosMutator(api as never, http as never);
  await mutate({ url: "/api/admin/status/", method: "GET" });
  for (const url of ["/api/client/events/", "/flags/", "https://example.invalid/api/admin/events/"]) {
    await mutate({ url, method: "POST", headers: { "X-CSRF-Token": firstToken, "X-Request-Id": "fixture" } });
  }
  await mutate({ url: "/api/admin/status/", method: "GET", headers: { "X-CSRF-Token": firstToken } });
  for (const request of [...api.requests, ...http.requests]) {
    assert.equal(tokenIn(request), undefined);
    assert.ok(!request.url?.includes(firstToken));
  }
  await assert.rejects(mutate({
    url: "/api/admin/events/", method: "POST", baseURL: "https://example.invalid/api",
  }), /same-origin API/);
});

test("an old status response cannot overwrite the token from a new login", async () => {
  let resolveStatus!: (value: unknown) => void;
  const api = sessionClient(async ({ url }) => url === "/admin/status/"
    ? new Promise((resolve) => { resolveStatus = resolve; })
    : { csrf_token: secondToken });
  const mutate = createAxiosMutator(api as never, sessionClient() as never);
  const pending = mutate({ url: "/api/admin/events/", method: "POST" });
  const rejected = assert.rejects(pending, /session changed/);
  await mutate({ url: "/api/admin/login/", method: "POST" });
  resolveStatus({ csrf_token: firstToken });
  await rejected;
  await mutate({ url: "/api/admin/events/", method: "POST" });
  assert.equal(api.requests.length, 3);
  assert.equal(tokenIn(api.requests[2]), secondToken);
});
