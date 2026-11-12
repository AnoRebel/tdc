-- Whether a person wants links in their conversations fetched.

-- Someone on metered data may reasonably want neither the outbound request nor
-- the picture it brings back. Defaults to on, because a preview is useful and
-- most people will never think about it.
ALTER TABLE users ADD COLUMN link_previews_enabled BOOLEAN NOT NULL DEFAULT true;
