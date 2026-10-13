SELECT o.id, COALESCE(p.name, 'no such product'), COALESCE(p.price, 0) FROM orders o LEFT JOIN products p ON p.id = o.product_id WHERE o.id % 113 = 0 OR o.id < 4 ORDER BY o.id;
