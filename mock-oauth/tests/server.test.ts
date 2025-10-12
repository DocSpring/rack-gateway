import assert from "node:assert/strict";
import test from "node:test";

import request from "supertest";

import { createApp } from "../server.js";

test("openid configuration endpoint", async () => {
  const app = await createApp();
  const response = await request(app).get("/.well-known/openid-configuration");
  assert.equal(response.status, 200);
  assert.equal(response.body.issuer.includes("http"), true);
  assert.equal(response.body.authorization_endpoint.includes("/oauth2/v2/auth"), true);
});

test("token endpoint rejects invalid code", async () => {
  const app = await createApp();
  const response = await request(app).post("/oauth2/v4/token").send({
    grant_type: "authorization_code",
    code: "invalid",
    redirect_uri: "http://localhost/test",
    client_id: "mock-client-id",
  });

  assert.equal(response.status, 400);
  assert.equal(response.body.error, "invalid_grant");
});
