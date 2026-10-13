SELECT c.id, c.name, COUNT(o.id) AS n FROM customers c LEFT JOIN orders o ON o.customer_id = c.id GROUP BY c.id, c.name HAVING COUNT(o.id) < 5 ORDER BY n, c.id;
