SELECT city, tier, COUNT(*), MIN(joined), MAX(joined) FROM customers GROUP BY city, tier ORDER BY city, tier;
