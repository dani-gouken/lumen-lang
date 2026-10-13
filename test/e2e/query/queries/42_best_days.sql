SELECT o.day, COUNT(*) AS orders, SUM(o.quantity) AS items FROM orders o WHERE o.day <= 60 GROUP BY o.day HAVING SUM(o.quantity) > 60 ORDER BY items DESC, o.day;
