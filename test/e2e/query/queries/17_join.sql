SELECT o.id, c.name, p.name, o.quantity FROM orders o JOIN customers c ON c.id = o.customer_id JOIN products p ON p.id = o.product_id WHERE o.id <= 20 ORDER BY o.id;
