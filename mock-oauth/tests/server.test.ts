import { expect, test } from "bun:test";
import request from "supertest";

import { createApp } from "../server.js";

test("openid configuration endpoint", async () => {
  const app = await createApp();
  const response = await request(app).get("/.well-known/openid-configuration");
  expect(response.status).toBe(200);
  expect(response.body.issuer.includes("http")).toBe(true);
  expect(response.body.authorization_endpoint.includes("/oauth2/v2/auth")).toBe(true);
});

test("token endpoint rejects invalid code", async () => {
  const app = await createApp();
  const response = await request(app).post("/oauth2/v4/token").send({
    grant_type: "authorization_code",
    code: "invalid",
    redirect_uri: "http://localhost/test",
    client_id: "mock-client-id",
  });

  expect(response.status).toBe(400);
  expect(response.body.error).toBe("invalid_grant");
});
