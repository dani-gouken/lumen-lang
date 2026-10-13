SELECT id, CASE WHEN discount > 0.2 THEN 'sale' END, CASE WHEN discount = 0 THEN quantity END FROM orders WHERE id <= 25 ORDER BY id;
