SELECT id, UPPER(name), LOWER(city), LENGTH(name), SUBSTR(name, 1, 3), SUBSTR(name, 5), SUBSTR(city, 2, 2) FROM customers WHERE id <= 12 ORDER BY id;
