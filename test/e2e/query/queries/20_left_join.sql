SELECT o.id, o.customer_id, c.name FROM orders o LEFT JOIN customers c ON c.id = o.customer_id WHERE c.name IS NULL ORDER BY o.id;
