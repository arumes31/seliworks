import { api, APIError } from "./api";
import type { Auth } from "./api";

export function passkeysSupported() {
  return (
    window.isSecureContext &&
    typeof PublicKeyCredential !== "undefined" &&
    !!navigator.credentials
  );
}

function decode(value: string): ArrayBuffer {
  const raw = atob(value.replace(/-/g, "+").replace(/_/g, "/"));
  return Uint8Array.from(raw, (c) => c.charCodeAt(0)).buffer;
}
function encode(value: ArrayBuffer) {
  return btoa(String.fromCharCode(...new Uint8Array(value)))
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
}
type Descriptor = Omit<PublicKeyCredentialDescriptor, "id"> & { id: string };
type Creation = Omit<
  PublicKeyCredentialCreationOptions,
  "challenge" | "user" | "excludeCredentials"
> & {
  challenge: string;
  user: Omit<PublicKeyCredentialUserEntity, "id"> & { id: string };
  excludeCredentials?: Descriptor[];
};
type Request = Omit<
  PublicKeyCredentialRequestOptions,
  "challenge" | "allowCredentials"
> & { challenge: string; allowCredentials?: Descriptor[] };

function serialize(credential: PublicKeyCredential) {
  const response = credential.response;
  const common = {
    id: credential.id,
    rawId: encode(credential.rawId),
    type: credential.type,
    authenticatorAttachment: credential.authenticatorAttachment,
    clientExtensionResults: credential.getClientExtensionResults(),
  };
  if (response instanceof AuthenticatorAttestationResponse)
    return {
      ...common,
      response: {
        clientDataJSON: encode(response.clientDataJSON),
        attestationObject: encode(response.attestationObject),
        transports: response.getTransports(),
      },
    };
  const assertion = response as AuthenticatorAssertionResponse;
  return {
    ...common,
    response: {
      clientDataJSON: encode(assertion.clientDataJSON),
      authenticatorData: encode(assertion.authenticatorData),
      signature: encode(assertion.signature),
      userHandle: assertion.userHandle ? encode(assertion.userHandle) : null,
    },
  };
}
async function ceremony(action: () => Promise<Credential | null>) {
  if (!passkeysSupported())
    throw new APIError(
      0,
      "Passkeys are not supported in this browser. Use your password.",
      "PASSKEY_UNSUPPORTED",
    );
  try {
    const credential = await action();
    if (!(credential instanceof PublicKeyCredential))
      throw new Error("No credential");
    return serialize(credential);
  } catch (error) {
    if (error instanceof DOMException && error.name === "InvalidStateError")
      throw new APIError(
        0,
        "This passkey is already registered. Choose another authenticator.",
        "PASSKEY_EXISTS",
      );
    throw new APIError(
      0,
      "Passkey operation cancelled or unavailable. Try again or use your password.",
      "PASSKEY_CANCELLED",
    );
  }
}
export async function registerPasskey(form: FormData) {
  const { publicKey } = await api<{ publicKey: Creation }>(
    "/me/passkeys/register/begin",
    "POST",
    {
      name: form.get("name"),
      password: form.get("password"),
      totp_code: form.get("totp_code") ?? "",
    },
  );
  const credential = await ceremony(() =>
    navigator.credentials.create({
      publicKey: {
        ...publicKey,
        challenge: decode(publicKey.challenge),
        user: { ...publicKey.user, id: decode(publicKey.user.id) },
        excludeCredentials: publicKey.excludeCredentials?.map((c) => ({
          ...c,
          id: decode(c.id),
        })),
      },
    }),
  );
  await api("/me/passkeys/register/finish", "POST", credential);
}
export async function loginWithPasskey(): Promise<Auth> {
  const { publicKey } = await api<{ publicKey: Request }>(
    "/auth/passkeys/begin",
    "POST",
    {},
  );
  const credential = await ceremony(() =>
    navigator.credentials.get({
      publicKey: {
        ...publicKey,
        challenge: decode(publicKey.challenge),
        allowCredentials: publicKey.allowCredentials?.map((c) => ({
          ...c,
          id: decode(c.id),
        })),
      },
    }),
  );
  return api<Auth>("/auth/passkeys/finish", "POST", credential);
}
