SELECT c.city, COUNT(*) AS orders, SUM(o.quantity) AS items FROM orders o JOIN customers c ON o.customer_id = c.id GROUP BY c.city ORDER BY items DESC, c.city;
