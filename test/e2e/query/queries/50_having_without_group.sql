SELECT COUNT(*), SUM(quantity) FROM orders WHERE day = 100 HAVING COUNT(*) > 2;
