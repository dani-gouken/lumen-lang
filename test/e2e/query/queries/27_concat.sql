SELECT name || ' (' || city || ', ' || joined || ')' , id || '-' || tier FROM customers WHERE id <= 10 ORDER BY id;
