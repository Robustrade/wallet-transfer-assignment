from django.db import migrations, models


class Migration(migrations.Migration):

    dependencies = [
        ('transfers', '0001_initial'),
    ]

    operations = [
        migrations.AddField(
            model_name='ledgerentries',
            name='amount',
            field=models.IntegerField(default=0),
            preserve_default=False,
        ),
    ]
