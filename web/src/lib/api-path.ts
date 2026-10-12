type PathValue = string | number

// pathSegment encodes a value (an app name, an email, an ID) as exactly one segment of an API
// path. Values come from the page's URL or from API responses, so without this a "/", "?" or "#"
// in one could send the request to a different endpoint than the code means. "." and ".." are
// refused: the browser resolves them however they're encoded, which would also change the endpoint.
export function pathSegment(value: PathValue): string {
  const segment = String(value)
  if (segment === '' || segment === '.' || segment === '..') {
    throw new Error(`Invalid API path segment: ${JSON.stringify(segment)}`)
  }
  return encodeURIComponent(segment)
}

// apiPath builds an API path, encoding every interpolated value as one path segment (see
// pathSegment): apiPath`/api/v1/apps/${app}/settings`. Pass a query string as the request's params.
export function apiPath(strings: TemplateStringsArray, ...values: PathValue[]): string {
  return strings.reduce((path, part, index) => path + pathSegment(values[index - 1]) + part)
}
