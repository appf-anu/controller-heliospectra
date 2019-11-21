# controller-heliospectra
go controller for heliospectra s7 and s10 lights

*multiplier* param and *MULTIPLIER* environment variable are used to change the value sent to the light

channels are sequentially numbered as such in conditions file:

    s7:
        channel-1  400nm
        channel-2  420nm
        channel-3  450nm
        channel-4  530nm
        channel-5  630nm
        channel-6  660nm
        channel-7  735nm
    s10:
        channel-1  370nm
        channel-2  400nm
        channel-3  420nm
        channel-4  450nm
        channel-5  530nm
        channel-6  620nm
        channel-7  660nm
        channel-8  735nm
        channel-9  850nm
        channel-10 6500k


