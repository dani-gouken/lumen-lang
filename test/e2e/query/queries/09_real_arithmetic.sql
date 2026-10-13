SELECT id, ROUND(price * 1.2, 2), ROUND(price / 3, 3), ROUND(price - 50, 1) FROM products WHERE id <= 12 ORDER BY id;
