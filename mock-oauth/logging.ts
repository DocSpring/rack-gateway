const DEFAULT_LEVEL = "info"

type Level = "debug" | "info" | "warn" | "error"

const LEVEL_ORDER: Record<Level, number> = {
  debug: 0,
  info: 1,
  warn: 2,
  error: 3,
}

const parseLevel = (raw: string | undefined, fallback: Level): Level => {
  const value = raw?.trim().toLowerCase()
  switch (value) {
    case "debug":
    case "trace":
      return "debug"
    case "info":
      return "info"
    case "warn":
    case "warning":
      return "warn"
    case "error":
    case "err":
      return "error"
    default:
      return fallback
  }
}

const parseTopics = (raw: string | undefined): { wildcard: boolean; exact: Set<string>; prefixes: string[] } => {
  const wildcard = { wildcard: false, exact: new Set<string>(), prefixes: [] as string[] }
  if (!raw) {
    return wildcard
  }

  const tokens = raw
    .split(/[;,]/)
    .map((token) => token.trim().toLowerCase())
    .filter(Boolean)

  for (const token of tokens) {
    if (token === "*" || token === "all") {
      wildcard.wildcard = true
      continue
    }
    if (token.endsWith(".*")) {
      const prefix = token.slice(0, -2)
      if (prefix) {
        wildcard.prefixes.push(`${prefix}.`)
      }
      continue
    }
    if (token.endsWith("*")) {
      const prefix = token.slice(0, -1)
      if (prefix) {
        wildcard.prefixes.push(prefix)
      }
      continue
    }
    wildcard.exact.add(token)
  }
  return wildcard
}

const truncate = (value: string, max = 2048): string => {
  if (value.length <= max) {
    return value
  }
  return `${value.slice(0, max)}…(truncated)`
}

export class Logger {
  private level: Level
  private topicsConfig: { wildcard: boolean; exact: Set<string>; prefixes: string[] }

  constructor(private readonly prefix: string) {
    this.level = parseLevel(process.env.LOG_LEVEL, DEFAULT_LEVEL)
    this.topicsConfig = parseTopics(process.env.DEBUG_TOPICS)
  }

  reload(): void {
    this.level = parseLevel(process.env.LOG_LEVEL, DEFAULT_LEVEL)
    this.topicsConfig = parseTopics(process.env.DEBUG_TOPICS)
  }

  topicEnabled(topic: string): boolean {
    const normalized = topic.trim().toLowerCase()
    if (!normalized) {
      return false
    }
    if (this.topicsConfig.wildcard) {
      return true
    }
    if (this.topicsConfig.exact.has(normalized)) {
      return true
    }
    for (const prefix of this.topicsConfig.prefixes) {
      if (normalized.startsWith(prefix)) {
        return true
      }
    }
    return false
  }

  debug(topic: string, message: string, ...args: unknown[]): void {
    if (LEVEL_ORDER[this.level] > LEVEL_ORDER.debug || !this.topicEnabled(topic)) {
      return
    }
    this.log("debug", topic, message, args)
  }

  info(message: string, ...args: unknown[]): void {
    if (LEVEL_ORDER[this.level] > LEVEL_ORDER.info) {
      return
    }
    this.log("info", undefined, message, args)
  }

  warn(message: string, ...args: unknown[]): void {
    if (LEVEL_ORDER[this.level] > LEVEL_ORDER.warn) {
      return
    }
    this.log("warn", undefined, message, args)
  }

  error(message: string, ...args: unknown[]): void {
    this.log("error", undefined, message, args)
  }

  private log(level: Level, topic: string | undefined, message: string, args: unknown[]): void {
    const parts = ["[", level.toUpperCase()]
    if (this.prefix) {
      parts.push(" ", this.prefix)
    }
    if (topic) {
      parts.push(" ", topic)
    }
    parts.push("]", " ", message)
    console.log(parts.join(""), ...args)
  }
}

export const truncateForLog = truncate

