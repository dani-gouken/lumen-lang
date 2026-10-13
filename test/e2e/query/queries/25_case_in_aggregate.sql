SELECT tier, SUM(CASE WHEN joined < 2015 THEN 1 ELSE 0 END) AS early, SUM(CASE WHEN joined >= 2015 THEN 1 ELSE 0 END) AS late FROM customers GROUP BY tier ORDER BY tier;
