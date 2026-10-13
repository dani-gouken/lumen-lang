SELECT p.category, ROUND(SUM(o.quantity * p.price * (1 - o.discount)), 2) AS revenue, COUNT(*) FROM orders o JOIN products p ON p.id = o.product_id GROUP BY p.category ORDER BY revenue DESC;
