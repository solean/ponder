/**
 * An API request that failed. `message` is ready to show to users: the
 * backend's `{ "error": "..." }` text when there is one, never the raw body.
 * `status` is the HTTP status, or 0 when the backend couldn't be reached.
 */
export class ApiError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

export const BACKEND_UNREACHABLE_MESSAGE = "Couldn't reach Ponder's backend.";

/** Longest non-JSON body worth showing as-is, such as Go's "404 page not found". */
const MAX_PLAIN_TEXT_MESSAGE = 200;

/**
 * Picks the user-facing message out of a failed response body. The backend
 * replies with `{ "error": "..." }`. Short plain-text bodies are kept. Other
 * JSON, HTML pages, and empty bodies return null.
 */
export function errorMessageFromBody(body: string): string | null {
  const text = body.trim();
  if (!text) return null;
  try {
    const parsed: unknown = JSON.parse(text);
    if (parsed && typeof parsed === "object" && "error" in parsed && typeof parsed.error === "string") {
      return parsed.error.trim() || null;
    }
    return null;
  } catch {
    // Not JSON: fall through to the plain-text check.
  }
  if (text.startsWith("<") || text.length > MAX_PLAIN_TEXT_MESSAGE) return null;
  return text;
}

export async function apiErrorFromResponse(res: Response): Promise<ApiError> {
  const body = await res.text().catch(() => "");
  const message = errorMessageFromBody(body) ?? `Request failed with status ${res.status}.`;
  return new ApiError(res.status, message);
}

/**
 * Sends a request and returns its JSON body. Throws ApiError for failed
 * responses and for network failures. Aborts are rethrown unchanged, so React
 * Query can recognise cancelled queries.
 */
export async function requestJSON<T>(url: string, init?: RequestInit): Promise<T> {
  let res: Response;
  try {
    res = await fetch(url, init);
  } catch (err) {
    if (init?.signal?.aborted) throw err;
    throw new ApiError(0, BACKEND_UNREACHABLE_MESSAGE);
  }
  if (!res.ok) {
    throw await apiErrorFromResponse(res);
  }
  return (await res.json()) as T;
}
