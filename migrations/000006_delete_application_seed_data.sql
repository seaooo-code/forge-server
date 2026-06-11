delete from applications
where id in (
    'app-mi-car',
    'app-mijia',
    'app-xiaoai',
    'app-miui-browser',
    'app-mi-fit',
    'app-mi-shop',
    'app-mi-pay',
    'app-mi-tv'
)
or slug in (
    'mi-car',
    'mijia',
    'xiaoai',
    'miui-browser',
    'mi-fit',
    'mi-shop',
    'mi-pay',
    'mi-tv'
);
