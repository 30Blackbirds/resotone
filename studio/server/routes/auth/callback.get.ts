import * as client from 'openid-client'

// The provider redirects back here. openid-client checks state, the PKCE
// verifier, the ID token's issuer, audience, expiry and nonce, and the `iss`
// response parameter when the provider sends one (RFC 9207). It does not check
// the ID token's signature for a token from the token endpoint, so verifyIdToken
// does, against the provider's published keys, before any claim is used.
export default defineEventHandler(async (event) => {
  const s = oidcSettings()
  if (!s) throw createError({ statusCode: 404 })

  const tx = await loginTransaction(event, s)
  const { verifier, state, nonce, next } = tx.data
  // One use only: whatever happens below, this transaction is spent.
  await tx.clear()
  if (!verifier || !state || !nonce) {
    // No transaction: an expired login, a second tab, or a callback nobody
    // started here. Start over rather than guess.
    return sendRedirect(event, '/login?reason=expired', 302)
  }

  // The URL the provider redirected to, rebuilt from configuration — never
  // from the Host header, which a proxy or a client can set.
  const current = new URL(redirectUri(s))
  current.search = new URL(getRequestURL(event)).search

  let tokens
  try {
    tokens = await client.authorizationCodeGrant(await oidcConfig(s), current, {
      pkceCodeVerifier: verifier,
      expectedState: state,
      expectedNonce: nonce,
      idTokenExpected: true,
    })
  } catch {
    return sendRedirect(event, '/login?reason=refused', 302)
  }

  let claims
  try {
    if (!tokens.id_token) throw new Error('no ID token')
    claims = await verifyIdToken(s, await oidcConfig(s), tokens.id_token, nonce)
  } catch {
    return sendRedirect(event, '/login?reason=refused', 302)
  }
  await saveTokens(event, s, tokens, {
    sub: claims.sub,
    email: typeof claims.email === 'string' ? claims.email : undefined,
    name: typeof claims.name === 'string' ? claims.name : undefined,
  })
  // Checked again here: a transaction stored before this rule existed was
  // checked by an older one.
  return sendRedirect(event, safeNext(next, s.editorUrl), 302)
})
