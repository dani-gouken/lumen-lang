SELECT id, NULLIF(discount, 0.0), NULLIF(quantity, 5), NULLIF(quantity, 5) IS NULL FROM orders WHERE id <= 20 ORDER BY id;
