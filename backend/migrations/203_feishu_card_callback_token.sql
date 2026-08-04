-- Separate Feishu card callback authentication from event subscription authentication.

INSERT INTO settings (key, value)
VALUES ('feishu_notify_card_verification_token', '')
ON CONFLICT (key) DO NOTHING;
