// Passes a Go API answer on to the browser: JSON as is, errors as {error} with the status the page can act on.
const KEEP = new Set([400, 404, 409, 429, 503]);

export async function forward(res: Response) {
  if (res.status === 204) return new Response(null, { status: 204 });
  if (res.ok) return Response.json(await res.json(), { headers: { "Cache-Control": "no-store" } });
  const text = (await res.text()).trim();
  // the API's own {"error": code} answers go through as they are, so the page can say what to fix
  try {
    const body = JSON.parse(text);
    if (typeof body?.error === "string") return Response.json(body, { status: KEEP.has(res.status) ? res.status : 502 });
  } catch {}
  return Response.json({ error: text || `API ${res.status}` }, { status: KEEP.has(res.status) ? res.status : 502 });
}

/** A route handler's try/catch around an API call: a 502 when the API can't be reached. */
export async function viaApi(call: () => Promise<Response>) {
  try {
    return await forward(await call());
  } catch (e) {
    return Response.json({ error: String((e as Error)?.message ?? e) }, { status: 502 });
  }
}
