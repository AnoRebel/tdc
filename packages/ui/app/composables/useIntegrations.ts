import type {
  CreatedIncomingWebhook,
  CreatedWebhook,
  IncomingWebhook,
  WebhookEventTypes,
  WebhookHistory,
  WebhookSubscription,
} from "@tdc/types";

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

  return {
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
