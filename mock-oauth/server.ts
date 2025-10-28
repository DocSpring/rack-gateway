import { createServer } from "node:http";
import { fileURLToPath, pathToFileURL } from "node:url";

import cors from "cors";
import express, { type Express, type Request, type Response } from "express";

import { DEFAULT_PORT, TOPICS } from "./constants.js";
import { logger } from "./logger.js";
import { truncateForLog } from "./logging.js";
import { registerRoutes } from "./routes.js";
import { ensureKeys } from "./signing.js";
import { mockUsers } from "./users.js";

const argv = process.argv.slice(2);
if (argv.includes("--help") || argv.includes("-h")) {
  logger.info(
    "Mock OAuth Server\n\nUsage: node dist/server.js [options]\n\nOptions:\n  --help, -h       Show this help message and exit.\n\nConfiguration:\n  MOCK_OAUTH_PORT    Port the HTTP server listens on (default: 3345).\n  PORT               Fallback port if MOCK_OAUTH_PORT not set.\n  OAUTH_ISSUER       Internal issuer URL override.\n  OAUTH_BROWSER_BASE Browser-facing base URL override.\n"
  );
  process.exit(0);
}

export async function createApp(): Promise<Express> {
  await ensureKeys();

  const app = express();
  app.use(cors());
  app.use(express.json());
  app.use(express.urlencoded({ extended: true }));

  app.use((req: Request, res: Response, next) => {
    if (logger.topicEnabled(TOPICS.http)) {
      logger.debug(TOPICS.http, `request ${req.method} ${req.originalUrl}`);
    }
    if (logger.topicEnabled(TOPICS.httpHeaders)) {
      for (const [key, value] of Object.entries(req.headers)) {
        logger.debug(TOPICS.httpHeaders, `${key}: ${String(value)}`);
      }
    }
    if (logger.topicEnabled(TOPICS.httpBody) && req.body && Object.keys(req.body).length > 0) {
      logger.debug(TOPICS.httpBody, truncateForLog(JSON.stringify(req.body)));
    }

    res.on("finish", () => {
      if (logger.topicEnabled(TOPICS.http)) {
        logger.debug(TOPICS.http, `response ${res.statusCode} ${req.method} ${req.originalUrl}`);
      }
    });

    next();
  });

  registerRoutes(app);
  return app;
}

export async function start(port = DEFAULT_PORT): Promise<void> {
  const app = await createApp();
  const server = createServer(app);
  server.listen(port, () => {
    logger.info(`Mock OAuth server running on port ${port}`);
    logger.info(`Visit http://localhost:${port} for endpoint info`);
    logger.debug(TOPICS.flow, `Mock users available: ${Object.keys(mockUsers).join(", ")}`);
  });
}

const modulePath = fileURLToPath(import.meta.url);
const invokedPath = fileURLToPath(pathToFileURL(process.argv[1] ?? "").href);

if (modulePath === invokedPath && process.env.SKIP_AUTOSTART !== "true") {
  const port = Number.parseInt(
    process.env.MOCK_OAUTH_PORT ?? process.env.PORT ?? String(DEFAULT_PORT),
    10
  );
  start(port).catch((error) => {
    logger.error("Failed to start mock OAuth server %o", error);
    process.exit(1);
  });
}

export const loggerInstance = logger;
export default createApp;
