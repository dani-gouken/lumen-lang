SELECT joined / 5 * 5 AS period, COUNT(*), ROUND(AVG(id), 2) FROM customers GROUP BY joined / 5 * 5 ORDER BY period;
