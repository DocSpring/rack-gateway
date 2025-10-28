import type { MockUser } from "./types.js";

export const mockUsers: Record<string, MockUser> = {
  "admin@example.com": {
    id: "1",
    email: "admin@example.com",
    name: "Admin User",
    picture: "https://via.placeholder.com/128",
    verified_email: true,
  },
  "deployer@example.com": {
    id: "2",
    email: "deployer@example.com",
    name: "Deployer User",
    picture: "https://via.placeholder.com/128",
    verified_email: true,
  },
  "ops@example.com": {
    id: "4",
    email: "ops@example.com",
    name: "Ops User",
    picture: "https://via.placeholder.com/128",
    verified_email: true,
  },
  "viewer@example.com": {
    id: "3",
    email: "viewer@example.com",
    name: "Viewer User",
    picture: "https://via.placeholder.com/128",
    verified_email: true,
  },
  "unauthorized@wrongdomain.com": {
    id: "999",
    email: "unauthorized@wrongdomain.com",
    name: "Unauthorized User",
    picture: "https://via.placeholder.com/128",
    verified_email: true,
  },
};
