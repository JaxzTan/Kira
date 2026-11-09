-- TR-14 / FR-7: seed categories on first run, in contract order.
-- Colors cycle --cat-1..--cat-5 (#0F766E #2F6FB5 #C2701E #7A5BB0 #9AA09B).
INSERT INTO categories (id, name, color) VALUES
  (1,  'Cloud & hosting',          '#0F766E'),
  (2,  'Software & subscriptions', '#2F6FB5'),
  (3,  'Hardware',                 '#C2701E'),
  (4,  'Internet & phone',         '#7A5BB0'),
  (5,  'Travel & transport',       '#9AA09B'),
  (6,  'Meals & entertainment',    '#0F766E'),
  (7,  'Professional fees',        '#2F6FB5'),
  (8,  'Marketing',                '#C2701E'),
  (9,  'Office',                   '#7A5BB0'),
  (10, 'Other',                    '#9AA09B');
