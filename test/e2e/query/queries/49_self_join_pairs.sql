SELECT a.name, b.name FROM products a JOIN products b ON a.category = b.category WHERE a.id < b.id AND a.price > 150 AND b.price > 150 ORDER BY a.id, b.id;
