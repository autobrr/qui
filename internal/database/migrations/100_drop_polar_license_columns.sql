-- Polar licensing is gone. Remove Polar licenses and their columns.

DELETE FROM licenses WHERE provider = 'polar';

ALTER TABLE licenses DROP COLUMN polar_customer_id;
ALTER TABLE licenses DROP COLUMN polar_product_id;
ALTER TABLE licenses DROP COLUMN polar_activation_id;
