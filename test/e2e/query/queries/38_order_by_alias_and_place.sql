SELECT city, tier, COUNT(*) AS n FROM customers GROUP BY city, tier ORDER BY n DESC, 1 DESC, tier LIMIT 15;
