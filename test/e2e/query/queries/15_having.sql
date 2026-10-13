SELECT city, COUNT(*) AS n FROM customers GROUP BY city HAVING COUNT(*) >= 30 ORDER BY n DESC, city;
