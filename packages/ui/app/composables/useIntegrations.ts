import type {
  Application,
  CreatedIncomingWebhook,
  CreatedWebhook,
  GrantablePermission,
  IncomingWebhook,
  InstalledApplication,
  IssuedApplicationCredential,
  WebhookEventTypes,
  WebhookHistory,
  WebhookSubscription,
} from "@tdc/types";

/** What registering an application asks for. */
export interface NewApplication {
  name: string;
  purpose: string;
  permissions: string[];
  sendsDataExternally: boolean;
  externalDisclosure: string;
}

/** What subscribing an endpoint asks for. */
export interface NewWebhook {
  roomId: string;
  label: string;
  url: string;
  eventTypes: string[];
}

/**
 * Programs acting on the server, as the person who set them up manages them.
 *
 * Every call goes through the same client as the rest of the application, so
 * the desktop's transport carries these unchanged.
 */
export function useIntegrations() {
  const api = useApi();

  function webhooks(): Promise<WebhookSubscription[]> {
    return api.get<WebhookSubscription[]>("/api/me/webhooks");
  }

  function webhook(id: string): Promise<WebhookHistory> {
    return api.get<WebhookHistory>(`/api/me/webhooks/${encodeURIComponent(id)}`);
  }

  /** Answers with the secret, which is never shown again. */
  function subscribe(request: NewWebhook): Promise<CreatedWebhook> {
    return api.post<CreatedWebhook>("/api/me/webhooks", request);
  }

  function unsubscribe(id: string): Promise<void> {
    return api.delete<void>(`/api/me/webhooks/${encodeURIComponent(id)}`);
  }

  function enable(id: string): Promise<WebhookSubscription> {
    return api.post<WebhookSubscription>(`/api/me/webhooks/${encodeURIComponent(id)}/enable`);
  }

  function eventTypes(): Promise<WebhookEventTypes> {
    return api.get<WebhookEventTypes>("/api/webhooks/event-types");
  }

  /** A channel's incoming webhooks; refused without the permission. */
  function incoming(roomId: string): Promise<IncomingWebhook[]> {
    return api.get<IncomingWebhook[]>(`/api/rooms/${roomId}/incoming-webhooks`);
  }

  /** Answers with the address to post to, which is never shown again. */
  function createIncoming(roomId: string, label: string): Promise<CreatedIncomingWebhook> {
    return api.post<CreatedIncomingWebhook>(`/api/rooms/${roomId}/incoming-webhooks`, { label });
  }

  function revokeIncoming(roomId: string, id: string): Promise<void> {
    return api.delete<void>(`/api/rooms/${roomId}/incoming-webhooks/${encodeURIComponent(id)}`);
  }

  /** Every application not removed, for whoever manages them. */
  function applications(): Promise<Application[]> {
    return api.get<Application[]>("/api/admin/applications");
  }

  function application(id: string): Promise<Application> {
    return api.get<Application>(`/api/admin/applications/${encodeURIComponent(id)}`);
  }

  function grantable(): Promise<GrantablePermission[]> {
    return api.get<GrantablePermission[]>("/api/admin/applications/permissions");
  }

  function register(request: NewApplication): Promise<Application> {
    return api.post<Application>("/api/admin/applications", request);
  }

  /**
   * Approves exactly the permissions the approver was shown, in the channels
   * chosen. Answers with the first credential, shown once.
   */
  function approve(
    id: string,
    permissions: string[],
    channels: string[],
  ): Promise<Application & { credential: IssuedApplicationCredential }> {
    return api.post(`/api/admin/applications/${encodeURIComponent(id)}/approve`, {
      permissions,
      channels,
    });
  }

  function restrict(id: string, channels: string[]): Promise<Application> {
    return api.put<Application>(`/api/admin/applications/${encodeURIComponent(id)}/channels`, {
      channels,
    });
  }

  function issueCredential(id: string): Promise<IssuedApplicationCredential> {
    return api.post(`/api/admin/applications/${encodeURIComponent(id)}/credentials`);
  }

  function revokeCredential(id: string, credential: string): Promise<void> {
    return api.delete(
      `/api/admin/applications/${encodeURIComponent(id)}/credentials/${encodeURIComponent(credential)}`,
    );
  }

  /** Removes it, keeping or taking down what it posted. */
  function remove(id: string, content: "retain" | "remove"): Promise<void> {
    return api.post(`/api/admin/applications/${encodeURIComponent(id)}/remove`, { content });
  }

  /** What is installed, as anybody may see it. */
  function installed(): Promise<InstalledApplication[]> {
    return api.get<InstalledApplication[]>("/api/applications");
  }

  /** The applications that can act in one channel. */
  function inRoom(roomId: string): Promise<InstalledApplication[]> {
    return api.get<InstalledApplication[]>(`/api/rooms/${roomId}/applications`);
  }

  return {
    applications,
    application,
    grantable,
    register,
    approve,
    restrict,
    issueCredential,
    revokeCredential,
    remove,
    installed,
    inRoom,
    webhooks,
    webhook,
    subscribe,
    unsubscribe,
    enable,
    eventTypes,
    incoming,
    createIncoming,
    revokeIncoming,
  };
}
