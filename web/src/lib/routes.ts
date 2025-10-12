const API_PREFIX = '/api/v1'
const WEB_PREFIX = '/app'

const join = (prefix: string, path = ''): string => {
  if (!path) {
    return prefix
  }
  if (path === '/') {
    return `${prefix}/`
  }
  if (path.startsWith('/')) {
    return `${prefix}${path}`
  }
  return `${prefix}/${path}`
}

export const APIRoute = (path = ''): string => join(API_PREFIX, path)
export const WebRoute = (path = ''): string => join(WEB_PREFIX, path)

export const DEFAULT_WEB_ROUTE = WebRoute('rack')
