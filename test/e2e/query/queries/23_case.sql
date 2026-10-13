SELECT id, quantity, CASE WHEN quantity >= 9 THEN 'bulk' WHEN quantity >= 4 THEN 'several' ELSE 'few' END FROM orders WHERE id BETWEEN 100 AND 125 ORDER BY id;
