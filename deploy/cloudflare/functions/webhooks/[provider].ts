interface Env {
  CONTROL_PLANE_BASE_URL: string;
  CONTROL_PLANE_PROXY_TOKEN?: string;
}

const providerPath: Record<string, string> = {
  paddle: "/webhooks/paddle",
};

export const onRequestPost: PagesFunction<Env> = async (ctx) => {
  const provider = ctx.params.provider as string;
  const upstreamPath = providerPath[provider];
  if (!upstreamPath) {
    return new Response(JSON.stringify({ error: "unsupported provider" }), {
      status: 404,
      headers: { "content-type": "application/json" },
    });
  }

  const body = await ctx.request.arrayBuffer();
  const headers = new Headers();
  headers.set("content-type", ctx.request.headers.get("content-type") ?? "application/json");

  const paddleSig = ctx.request.headers.get("paddle-signature");
  if (paddleSig) headers.set("paddle-signature", paddleSig);

  if (ctx.env.CONTROL_PLANE_PROXY_TOKEN) {
    headers.set("x-webhook-proxy-token", ctx.env.CONTROL_PLANE_PROXY_TOKEN);
  }

  const upstream = new URL(upstreamPath, ctx.env.CONTROL_PLANE_BASE_URL).toString();
  const res = await fetch(upstream, {
    method: "POST",
    headers,
    body,
  });

  return new Response(await res.text(), {
    status: res.status,
    headers: { "content-type": res.headers.get("content-type") ?? "application/json" },
  });
};
