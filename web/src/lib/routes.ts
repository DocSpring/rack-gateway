const API_PREFIX = '/api/v1';
const WEB_PREFIX = '/app';

const join = (prefix: string, path = ''): string => {
  if (!path) {
    return prefix;
  }
  if (path === '/') {
    return `${prefix}/`;
  }
  if (path.startsWith('/')) {
    return `${prefix}${path}`;
  }
  return `${prefix}/${path}`;
};

export const APIRoute = (path = ''): string => join(API_PREFIX, path);
export const WebRoute = (path = ''): string => join(WEB_PREFIX, path);

export const DEFAULT_WEB_ROUTE = WebRoute('rack');

const SUFFIX_SEPARATOR = /[?#]/;

const splitPathAndSuffix = (value: string): [string, string] => {
  const index = value.search(SUFFIX_SEPARATOR);
  if (index === -1) {
    return [value, ''];
  }
  return [value.slice(0, index), value.slice(index)];
};

export const resolveWebRedirect = (target?: string | null): string => {
  if (!target) {
    return DEFAULT_WEB_ROUTE;
  }

  const trimmed = target.trim();
  if (trimmed === '') {
    return DEFAULT_WEB_ROUTE;
  }

  const [pathPart, suffix] = splitPathAndSuffix(trimmed);
  let relativePath = pathPart;

  if (relativePath === WEB_PREFIX) {
    relativePath = '';
  } else if (relativePath.startsWith(`${WEB_PREFIX}/`)) {
    relativePath = relativePath.slice(WEB_PREFIX.length + 1);
  } else if (relativePath.startsWith('/')) {
    relativePath = relativePath.slice(1);
  }

  const resolved = WebRoute(relativePath);
  return `${resolved}${suffix}`;
};
