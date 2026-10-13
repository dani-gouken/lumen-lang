SELECT a.id, b.id FROM products a JOIN products b ON a.id + 1 = b.id AND a.category = b.category WHERE a.id <= 40 ORDER BY a.id;
