-- Remove the short-lived duplicate card token setting; Feishu uses one encryption policy for events and callbacks.

DELETE FROM settings
WHERE key = 'feishu_notify_card_verification_token';
