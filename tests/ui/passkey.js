// A software passkey. Chromium's DevTools protocol can attach a virtual
// CTAP2 authenticator that answers navigator.credentials.create/get on
// its own — a resident key, user verification always "yes", presence
// simulated — so the panel's real sign-in runs end to end with nobody
// touching a phone. Nothing in the hub knows it is not a real device.
export async function attachPasskey(page) {
  const cdp = await page.context().newCDPSession(page);
  await cdp.send('WebAuthn.enable');
  const { authenticatorId } = await cdp.send('WebAuthn.addVirtualAuthenticator', {
    options: {
      protocol: 'ctap2',
      transport: 'internal',
      hasResidentKey: true,
      hasUserVerification: true,
      isUserVerified: true,
      automaticPresenceSimulation: true,
    },
  });
  return { cdp, authenticatorId };
}
