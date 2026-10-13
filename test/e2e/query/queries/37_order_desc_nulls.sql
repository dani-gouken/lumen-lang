SELECT o.id, p.price FROM orders o LEFT JOIN products p ON p.id = o.product_id WHERE o.id % 113 = 0 OR o.id IN (1, 2, 3) ORDER BY p.price DESC, o.id DESC;
